package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"payment.service/paypal"
)

type testPayPalGateway struct{}

func (testPayPalGateway) CreateOrder(_ context.Context, _, _, _ string) (paypal.Order, error) {
	return paypal.Order{ID: "paypal-order", ApprovalURL: "https://example.com/approve"}, nil
}

func (testPayPalGateway) CaptureOrder(_ context.Context, _ string) (paypal.Capture, error) {
	return paypal.Capture{ID: "paypal-capture", Status: "COMPLETED"}, nil
}

func TestSessionValidationAndOwnership(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != "user-a" && token != "user-b" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user":       map[string]string{"id": token, "email": token + "@example.com"},
			"expires_at": "2030-01-01T12:00:00Z",
		})
	}))
	defer authServer.Close()
	t.Setenv("AUTH_SERVICE_URL", authServer.URL)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://shop.example")

	api := NewHandler(testPayPalGateway{})

	currentSession := sendRequest(api, http.MethodGet, "/sessions/current", "", "user-a")
	if currentSession.Code != http.StatusOK || !strings.Contains(currentSession.Body.String(), `"id":"user-a"`) {
		t.Fatalf("session response = %d %s, want 200 with validated user", currentSession.Code, currentSession.Body.String())
	}

	unauthorized := sendRequest(api, http.MethodPost, "/orders", `{"items":[]}`, "invalid")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized order status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	createdOrder := sendRequest(api, http.MethodPost, "/orders", `{"items":[{"product_id":"book-1","name":"Book","quantity":1,"unit_price":12.5}]}`, "user-a")
	if createdOrder.Code != http.StatusCreated {
		t.Fatalf("create order status = %d, body = %s", createdOrder.Code, createdOrder.Body.String())
	}
	var order struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(createdOrder.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}

	foreignOrder := sendRequest(api, http.MethodGet, "/orders/"+order.OrderID, "", "user-b")
	if foreignOrder.Code != http.StatusNotFound {
		t.Fatalf("foreign order status = %d, want %d", foreignOrder.Code, http.StatusNotFound)
	}

	createdPayment := sendRequest(api, http.MethodPost, "/orders/"+order.OrderID+"/pay", "", "user-a")
	if createdPayment.Code != http.StatusCreated {
		t.Fatalf("create payment status = %d, body = %s", createdPayment.Code, createdPayment.Body.String())
	}
	var payment struct {
		PaymentID string `json:"payment_id"`
	}
	if err := json.Unmarshal(createdPayment.Body.Bytes(), &payment); err != nil {
		t.Fatal(err)
	}

	foreignPayment := sendRequest(api, http.MethodGet, "/payments/"+payment.PaymentID, "", "user-b")
	if foreignPayment.Code != http.StatusNotFound {
		t.Fatalf("foreign payment status = %d, want %d", foreignPayment.Code, http.StatusNotFound)
	}
}

func TestCORSPreflight(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://shop.example")
	api := NewHandler(nil)
	request := httptest.NewRequest(http.MethodOptions, "/orders", nil)
	request.Header.Set("Origin", "https://shop.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("preflight did not enable credentials for the configured origin")
	}
}

func sendRequest(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
