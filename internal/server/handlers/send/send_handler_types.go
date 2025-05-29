package send

import (
	"encoding/json"
	"fmt"

	"github.com/openmined/syftbox/internal/utils"
)

type PollStatus string

const (
	PollStatusPending  PollStatus = "pending"
	PollStatusComplete PollStatus = "complete"
)

// Error constants
const (
	ErrorTimeout        = "timeout"
	ErrorInvalidRequest = "invalid_request"
	ErrorInternal       = "internal_error"
	ErrorNotFound       = "not_found"
)

// APIError represents a standardized error response
type APIError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// APIResponse represents a standardized success response
type APIResponse struct {
	RequestID string      `json:"request_id"`
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message,omitempty"`
}

type PollInfo struct {
	PollURL string `json:"poll_url"`
}

type JSONHeaders map[string]string

func (h *JSONHeaders) UnmarshalText(text []byte) error {
	var headers map[string]string
	if err := json.Unmarshal(text, &headers); err != nil {
		return fmt.Errorf("invalid JSON in headers: %v", err)
	}
	*h = JSONHeaders(headers)
	return nil
}

// MessageRequest represents the request for sending a message
type MessageRequest struct {
	SyftURL utils.SyftBoxURL `form:"x-syft-url" binding:"required"`
	From    string           `form:"x-syft-from" binding:"required"`
	Headers JSONHeaders      `header:"x-syft-headers"`
	Timeout int              `form:"timeout" binding:"gte=0"`
	Method  string           `form:"method"`
}

// PollObjectRequest represents the request for polling
type PollObjectRequest struct {
	RequestID string `form:"request_id" binding:"required"`
	AppName   string `form:"app_name" binding:"required"`
	AppEp     string `form:"app_endpoint" binding:"required"`
	User      string `form:"user" binding:"required"`
	Timeout   int    `form:"timeout,omitempty" binding:"gte=0"`
}

// SendResult represents the result of a send operation
type SendResult struct {
	Status    int
	RequestID string
	PollURL   string
	Response  map[string]interface{}
}

// PollResult represents the result of a poll operation
type PollResult struct {
	Status    int
	RequestID string
	Response  map[string]interface{}
}
