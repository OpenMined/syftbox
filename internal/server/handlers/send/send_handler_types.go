package send

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

// MessageRequest represents the request for sending a message
type MessageRequest struct {
	From    string            `header:"x-syft-from" binding:"required"`
	To      string            `header:"x-syft-to" binding:"required"`
	AppName string            `header:"x-syft-app" binding:"required"`
	AppEp   string            `header:"x-syft-appep" binding:"required"`
	Headers map[string]string `header:"x-syft-headers"`
	Status  int               `header:"x-syft-status"`
	Timeout int               `form:"timeout" header:"x-syft-timeout" binding:"gte=0"`
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
