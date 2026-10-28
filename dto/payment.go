package dto

import "time"

type Payment struct {
	PaymentID       string    `json:"payment_id"`
	OrderID         string    `json:"order_id"`
	Amount          float64   `json:"amount"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	PayPalOrderID   string    `json:"paypal_order_id"`
	PayPalCaptureID string    `json:"paypal_capture_id,omitempty"`
	ApprovalURL     string    `json:"approval_url,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}
