package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"payment.service/dto"
	"payment.service/logger"
	"payment.service/paypal"
)

type paypalGateway interface {
	CreateOrder(context.Context, string, string, string) (paypal.Order, error)
	CaptureOrder(context.Context, string) (paypal.Capture, error)
}

type memoryStore struct {
	mu             sync.Mutex
	orders         map[string]dto.Order
	payments       map[string]dto.Payment
	paymentByOrder map[string]string
}

type handler struct {
	provider paypalGateway
	store    *memoryStore
}

func Server() error {
	clientID := os.Getenv("PAYPAL_CLIENT_ID")
	clientSecret := os.Getenv("PAYPAL_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("set PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET before starting")
	}

	baseURL := paypalSandboxURL()
	if strings.EqualFold(os.Getenv("PAYPAL_ENV"), "live") {
		baseURL = "https://api-m.paypal.com"
	} else if env := os.Getenv("PAYPAL_ENV"); env != "" && !strings.EqualFold(env, "sandbox") {
		return fmt.Errorf("PAYPAL_ENV must be sandbox or live")
	}

	client, err := paypal.NewClient(paypal.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		BaseURL:      baseURL,
	})
	if err != nil {
		return err
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("invalid PORT: %s", port)
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           NewHandler(client),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("payment service listening on :%s (PayPal %s)", port, strings.ToLower(defaultEnv(os.Getenv("PAYPAL_ENV"), "sandbox")))
	return server.ListenAndServe()
}

func paypalSandboxURL() string {
	return "https://api-m.sandbox.paypal.com"
}

func defaultEnv(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func NewHandler(provider paypalGateway) http.Handler {
	h := &handler{
		provider: provider,
		store: &memoryStore{
			orders:         make(map[string]dto.Order),
			payments:       make(map[string]dto.Payment),
			paymentByOrder: make(map[string]string),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /docs", docsHandler)
	mux.HandleFunc("POST /orders", h.createOrder)
	mux.HandleFunc("GET /orders/{id}", h.getOrder)
	mux.HandleFunc("POST /orders/{id}/pay", h.createPayment)
	mux.HandleFunc("POST /payments/{id}/capture", h.capturePayment)
	mux.HandleFunc("GET /payments/{id}", h.getPayment)
	return correlationMiddleware(mux)
}

func correlationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id == "" {
			id = newID()
		}
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), correlationKey{}, id)))
	})
}

type correlationKey struct{}

func requestLogger(r *http.Request) *logger.Logger {
	id, _ := r.Context().Value(correlationKey{}).(string)
	return logger.New(id)
}

func (h *handler) createOrder(w http.ResponseWriter, r *http.Request) {
	requestLogger(r).Info("create order request")

	var request struct {
		Items    []dto.Item `json:"items"`
		Currency string     `json:"currency"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(request.Items) == 0 || len(request.Items) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_items", "items must contain between 1 and 100 entries")
		return
	}

	currency := strings.ToUpper(strings.TrimSpace(request.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 || currency < "AAA" || currency > "ZZZ" {
		writeError(w, http.StatusBadRequest, "invalid_currency", "currency must be a three-letter code")
		return
	}

	var totalCents int64
	for i := range request.Items {
		item := &request.Items[i]
		item.ProductID = strings.TrimSpace(item.ProductID)
		item.Name = strings.TrimSpace(item.Name)
		if item.ProductID == "" || item.Name == "" || item.Quantity < 1 || item.Quantity > 10000 {
			writeError(w, http.StatusBadRequest, "invalid_item", "each item needs a product_id, name, and valid quantity")
			return
		}
		if math.IsNaN(item.UnitPrice) || math.IsInf(item.UnitPrice, 0) || item.UnitPrice <= 0 || item.UnitPrice > 1_000_000 {
			writeError(w, http.StatusBadRequest, "invalid_price", "unit_price must be a positive amount")
			return
		}

		unitCents := int64(math.Round(item.UnitPrice * 100))
		subtotalCents := unitCents * int64(item.Quantity)
		totalCents += subtotalCents
		item.UnitPrice = float64(unitCents) / 100
		item.Subtotal = float64(subtotalCents) / 100
	}
	if totalCents <= 0 || totalCents > 99_999_999_999 {
		writeError(w, http.StatusBadRequest, "invalid_total", "order total is outside the supported range")
		return
	}

	order := dto.Order{
		OrderID:   newID(),
		Items:     request.Items,
		Total:     float64(totalCents) / 100,
		Currency:  currency,
		Status:    "CREATED",
		CreatedAt: time.Now().UTC(),
	}

	h.store.mu.Lock()
	h.store.orders[order.OrderID] = order
	h.store.mu.Unlock()
	writeJSON(w, http.StatusCreated, order)
}

func (h *handler) getOrder(w http.ResponseWriter, r *http.Request) {
	requestLogger(r).Info("get order request")
	h.store.mu.Lock()
	order, ok := h.store.orders[r.PathValue("id")]
	h.store.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "order_not_found", "order was not found")
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (h *handler) createPayment(w http.ResponseWriter, r *http.Request) {
	requestLogger(r).Info("create payment request")
	orderID := r.PathValue("id")

	// Serialize payment creation so repeated requests do not create duplicate PayPal orders.
	h.store.mu.Lock()
	defer h.store.mu.Unlock()

	order, ok := h.store.orders[orderID]
	if !ok {
		writeError(w, http.StatusNotFound, "order_not_found", "order was not found")
		return
	}
	if existingID := h.store.paymentByOrder[orderID]; existingID != "" {
		existing := h.store.payments[existingID]
		if existing.Status == "PENDING" || existing.Status == "COMPLETED" {
			writeJSON(w, http.StatusOK, existing)
			return
		}
	}

	remote, err := h.provider.CreateOrder(
		r.Context(),
		order.OrderID,
		order.Currency,
		formatAmount(order.Total),
	)
	if err != nil {
		requestLogger(r).Error("PayPal order creation failed")
		writeError(w, http.StatusBadGateway, "paypal_error", "could not create PayPal order")
		return
	}

	payment := dto.Payment{
		PaymentID:     newID(),
		OrderID:       order.OrderID,
		Amount:        order.Total,
		Currency:      order.Currency,
		Status:        "PENDING",
		PayPalOrderID: remote.ID,
		ApprovalURL:   remote.ApprovalURL,
		CreatedAt:     time.Now().UTC(),
	}
	h.store.payments[payment.PaymentID] = payment
	h.store.paymentByOrder[orderID] = payment.PaymentID
	writeJSON(w, http.StatusCreated, payment)
}

func (h *handler) capturePayment(w http.ResponseWriter, r *http.Request) {
	requestLogger(r).Info("capture payment request")
	id := r.PathValue("id")

	h.store.mu.Lock()
	defer h.store.mu.Unlock()

	payment, ok := h.store.payments[id]
	if !ok {
		writeError(w, http.StatusNotFound, "payment_not_found", "payment was not found")
		return
	}
	if payment.Status == "COMPLETED" {
		writeJSON(w, http.StatusOK, payment)
		return
	}

	result, err := h.provider.CaptureOrder(r.Context(), payment.PayPalOrderID)
	if err != nil {
		requestLogger(r).Error("PayPal capture failed")
		writeError(w, http.StatusBadGateway, "paypal_error", "could not capture PayPal payment")
		return
	}

	payment.Status = strings.ToUpper(result.Status)
	payment.PayPalCaptureID = result.ID
	h.store.payments[id] = payment

	if payment.Status == "COMPLETED" {
		order := h.store.orders[payment.OrderID]
		order.Status = "PAID"
		h.store.orders[payment.OrderID] = order
	}

	writeJSON(w, http.StatusOK, payment)
}

func (h *handler) getPayment(w http.ResponseWriter, r *http.Request) {
	requestLogger(r).Info("get payment request")
	h.store.mu.Lock()
	payment, ok := h.store.payments[r.PathValue("id")]
	h.store.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "payment_not_found", "payment was not found")
		return
	}
	writeJSON(w, http.StatusOK, payment)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request must contain one JSON object")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, dto.APIError{
		Error:     code,
		Message:   message,
		CreatedAt: time.Now().UTC(),
		Code:      status,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(id[:])
}

func formatAmount(amount float64) string {
	cents := int64(math.Round(amount * 100))
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
