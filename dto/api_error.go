package dto

import "time"

type APIError struct {
	Error     string    `json:"error"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
	Code      int       `json:"error_code"`
}
