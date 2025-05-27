package send

type PollStatus string

const (
	PollStatusPending  PollStatus = "pending"
	PollStatusComplete PollStatus = "complete"
)

// SendAcknowledgment represents the response for a successful message send request
type SendAcknowledgment struct {
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	PollURL   string `json:"poll_url"`
}

// SendError represents the error response for a failed message send request
type SendError struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
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

type PollObjectRequest struct {
	RequestID string `form:"request_id" binding:"required"`
	AppName   string `form:"app_name" binding:"required"`
	AppEp     string `form:"app_endpoint" binding:"required"`
	User      string `form:"user" binding:"required"`
	Timeout   int    `form:"timeout,omitempty" binding:"gte=0"`
}

type PollResponse struct {
	Message    map[string]interface{} `json:"message"`
	RequestID  string                 `json:"request_id"`
	PollStatus PollStatus             `json:"poll_status"`
}

type PollError struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
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
