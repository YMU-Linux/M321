package dto

import "time"

type Order struct {
	OrderID   string    `json:"order_id"`
	Items     []Item    `json:"items"`
	Total     float64   `json:"total"`
	Currency  string    `json:"currency"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
