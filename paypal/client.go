package paypal

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
    "sync"
    "time"
)

const sandboxURL = "https://api-m.sandbox.paypal.com"

type Config struct {
    ClientID     string
    ClientSecret string
    BaseURL      string
}

type Client struct {
    client       *http.Client
    clientID     string
    clientSecret string
    baseURL      string

    mu          sync.Mutex
    accessToken string
    tokenExpiry time.Time
}

type Order struct {
    ID         string
    ApprovalURL string
}

type Capture struct {
    ID     string
    Status string
}

func NewClient(cfg Config) (*Client, error) {
    if cfg.ClientID == "" || cfg.ClientSecret == "" {
        return nil, fmt.Errorf("PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET are required")
    }
    if cfg.BaseURL == "" {
        cfg.BaseURL = sandboxURL
    }
    cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

    return &Client{
        client:       &http.Client{Timeout: 20 * time.Second},
        clientID:     cfg.ClientID,
        clientSecret: cfg.ClientSecret,
        baseURL:      cfg.BaseURL,
    }, nil
}

func (c *Client) CreateOrder(ctx context.Context, referenceID, currency, amount string) (Order, error) {
    body := map[string]any{
        "intent": "CAPTURE",
        "purchase_units": []any{
            map[string]any{
                "reference_id": referenceID,
                "amount": map[string]string{
                    "currency_code": currency,
                    "value":         amount,
                },
            },
        },
        "application_context": map[string]string{
            "user_action": "PAY_NOW",
        },
    }

    var response struct {
        ID    string `json:"id"`
        Links []struct {
            Rel  string `json:"rel"`
            Href string `json:"href"`
        } `json:"links"`
    }
    if err := c.apiRequest(ctx, http.MethodPost, "/v2/checkout/orders", body, &response); err != nil {
        return Order{}, err
    }

    result := Order{ID: response.ID}
    for _, link := range response.Links {
        if link.Rel == "approve" || link.Rel == "payer-action" {
            result.ApprovalURL = link.Href
            break
        }
    }
    if result.ID == "" || result.ApprovalURL == "" {
        return Order{}, fmt.Errorf("PayPal response did not contain an order ID and approval URL")
    }
    return result, nil
}

func (c *Client) CaptureOrder(ctx context.Context, orderID string) (Capture, error) {
    var response struct {
        Status       string `json:"status"`
        PurchaseUnits []struct {
            Payments struct {
                Captures []struct {
                    ID     string `json:"id"`
                    Status string `json:"status"`
                } `json:"captures"`
            } `json:"payments"`
        } `json:"purchase_units"`
    }

    path := "/v2/checkout/orders/" + url.PathEscape(orderID) + "/capture"
    if err := c.apiRequest(ctx, http.MethodPost, path, map[string]string{}, &response); err != nil {
        return Capture{}, err
    }

    result := Capture{Status: response.Status}
    if len(response.PurchaseUnits) > 0 &&
        len(response.PurchaseUnits[0].Payments.Captures) > 0 {
        result.ID = response.PurchaseUnits[0].Payments.Captures[0].ID
        result.Status = response.PurchaseUnits[0].Payments.Captures[0].Status
    }
    if result.Status == "" {
        return Capture{}, fmt.Errorf("PayPal capture response did not contain a status")
    }
    return result, nil
}

func (c *Client) apiRequest(ctx context.Context, method, path string, body any, output any) error {
    token, err := c.getAccessToken(ctx)
    if err != nil {
        return err
    }

    data, err := json.Marshal(body)
    if err != nil {
        return fmt.Errorf("encode PayPal request: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(data))
    if err != nil {
        return fmt.Errorf("create PayPal request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/json")

    resp, err := c.client.Do(req)
    if err != nil {
        return fmt.Errorf("call PayPal API: %w", err)
    }
    defer resp.Body.Close()

    responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
    if err != nil {
        return fmt.Errorf("read PayPal response: %w", err)
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return fmt.Errorf("PayPal API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
    }
    if output == nil || len(responseBody) == 0 {
        return nil
    }
    if err := json.Unmarshal(responseBody, output); err != nil {
        return fmt.Errorf("decode PayPal response: %w", err)
    }
    return nil
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if c.accessToken != "" && time.Now().Add(30*time.Second).Before(c.tokenExpiry) {
        return c.accessToken, nil
    }

    req, err := http.NewRequestWithContext(
        ctx,
        http.MethodPost,
        c.baseURL+"/v1/oauth2/token",
        strings.NewReader("grant_type=client_credentials"),
    )
    if err != nil {
        return "", fmt.Errorf("create PayPal token request: %w", err)
    }
    req.SetBasicAuth(c.clientID, c.clientSecret)
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    req.Header.Set("Accept", "application/json")

    resp, err := c.client.Do(req)
    if err != nil {
        return "", fmt.Errorf("request PayPal access token: %w", err)
    }
    defer resp.Body.Close()

    responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
    if err != nil {
        return "", fmt.Errorf("read PayPal token response: %w", err)
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return "", fmt.Errorf("PayPal token endpoint returned HTTP %d", resp.StatusCode)
    }

    var result struct {
        AccessToken string `json:"access_token"`
        ExpiresIn   int    `json:"expires_in"`
    }
    if err := json.Unmarshal(responseBody, &result); err != nil {
        return "", fmt.Errorf("decode PayPal token response: %w", err)
    }
    if result.AccessToken == "" {
        return "", fmt.Errorf("PayPal token response was empty")
    }

    c.accessToken = result.AccessToken
    c.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
    return c.accessToken, nil
}