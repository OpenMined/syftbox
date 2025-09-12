package send

import (
	"context"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/utils"
)

type PollStatus string

const (
	PollStatusPending  PollStatus = "pending"
	PollStatusComplete PollStatus = "complete"
)

// Error constants
const (
	ErrorTimeout          = "timeout"
	ErrorInvalidRequest   = "invalid_request"
	ErrorInternal         = "internal_error"
	ErrorNotFound         = "not_found"
	ErrorPermissionDenied = "permission_denied"
	PollURL               = "/api/v1/send/poll?x-syft-request-id=%s&x-syft-url=%s&x-syft-from=%s&x-syft-raw=%t"
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

type Headers map[string]string

// MessageRequest represents the request for sending a message
type MessageRequest struct {
	SyftURL      utils.SyftBoxURL `form:"x-syft-url" binding:"required"`  // Binds to the syft url using UnmarshalParam
	From         string           `form:"x-syft-from" binding:"required"` // The sender of the message
	Timeout      int              `form:"timeout" binding:"gte=0"`        // The timeout for the request in milliseconds
	AsRaw        bool             `form:"x-syft-raw" default:"false"`     // If true, the request body will be read and sent as is
	Method       string           // Will be set from request method
	Headers      Headers          // Will be set from request headers
	SuffixSender bool             `form:"suffix-sender" default:"false"` // If true, the sender prefix will be added to the request
}

func (h *MessageRequest) BindHeaders(ctx *gin.Context) {
	h.Headers = make(Headers)
	for k, v := range ctx.Request.Header {
		if len(v) > 0 {
			// Convert header key to lowercase
			lowerKey := strings.ToLower(k)

			// Skip the Authorization header as it's used for Syftbox authentication
			// and should not be forwarded to the RPC endpoint
			if lowerKey == "authorization" {
				continue
			}

			h.Headers[lowerKey] = v[0]
		}
	}
	// Normalize guest identity: prefer guest@syftbox.net but accept legacy guest@syft.org
	fromLower := strings.ToLower(h.From)
	if fromLower == "guest@syft.org" {
		h.From = "guest@syftbox.net"
	}
	// Bind x-syft-from to Headers (already lowercase)
	h.Headers["x-syft-from"] = h.From
}

// PollObjectRequest represents the request for polling
type PollObjectRequest struct {
	RequestID string           `form:"x-syft-request-id" binding:"required"`
	From      string           `form:"x-syft-from" binding:"required"`
	SyftURL   utils.SyftBoxURL `form:"x-syft-url" binding:"required"`
	Timeout   int              `form:"timeout,omitempty" binding:"gte=0"` // Timeout in milliseconds
	UserAgent string           `form:"user-agent,omitempty"`
	AsRaw     bool             `form:"x-syft-raw" default:"false"` // If true, the request body will be read and sent as is
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

// Message store interface for storing and retrieving messages
type RPCMsgStore interface {
	StoreMsg(ctx context.Context, path string, msgBytes []byte) error
	GetMsg(ctx context.Context, path string) (io.ReadCloser, error)
	DeleteMsg(ctx context.Context, path string) error
}

// Message dispatch interface for dispatching messages to users
type MessageDispatcher interface {
	Dispatch(datasite string, msg *syftmsg.Message) bool
}

// SendServiceInterface defines the interface for the send service
type SendServiceInterface interface {
	SendMessage(ctx context.Context, req *MessageRequest, bodyBytes []byte) (*SendResult, error)
	PollForResponse(ctx context.Context, req *PollObjectRequest) (*PollResult, error)
	constructPollURL(requestID string, syftURL utils.SyftBoxURL, from string, asRaw bool) string
	GetConfig() *Config
}
