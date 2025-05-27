package send

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
)

// SendHandler handles HTTP requests for sending messages
type SendHandler struct {
	service *SendService
}

// New creates a new send handler
func New(hub *ws.WebsocketHub, blob *blob.BlobService) *SendHandler {
	service := NewSendService(hub, blob, nil)
	return &SendHandler{service: service}
}

// SendMsg handles sending a message
func (h *SendHandler) SendMsg(ctx *gin.Context) {
	var req MessageRequest
	if err := ctx.ShouldBindHeader(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Read request body with size limit
	bodyBytes, err := readRequestBody(ctx, h.service.cfg.MaxBodySize)
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.service.SendMessage(ctx.Request.Context(), &req, bodyBytes)
	if err != nil {
		slog.Error("failed to send message", "error", err)
		ctx.PureJSON(http.StatusInternalServerError, SendError{
			Error:     err.Error(),
			RequestID: result.RequestID,
		})
		return
	}

	if result.Response != nil {
		ctx.PureJSON(result.Status, PollResponse{
			Message:    result.Response,
			RequestID:  result.RequestID,
			PollStatus: PollStatusComplete,
		})
		return
	}

	ctx.PureJSON(result.Status, SendAcknowledgment{
		Message:   "Request has been accepted. Please check back later.",
		RequestID: result.RequestID,
		PollURL:   result.PollURL,
	})
}

// PollForResponse handles polling for a response
func (h *SendHandler) PollForResponse(ctx *gin.Context) {
	var req PollObjectRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		slog.Error("failed to bind query parameters", "error", err)
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.service.PollForResponse(ctx.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, ErrPollTimeout) {
			ctx.PureJSON(http.StatusOK, PollResponse{
				Message:    nil,
				RequestID:  req.RequestID,
				PollStatus: PollStatusPending,
			})
			return
		}

		slog.Error("failed to poll for response", "error", err)
		ctx.PureJSON(http.StatusInternalServerError, PollError{
			Error:     err.Error(),
			RequestID: req.RequestID,
		})
		return
	}

	ctx.PureJSON(result.Status, PollResponse{
		Message:    result.Response,
		RequestID:  result.RequestID,
		PollStatus: PollStatusComplete,
	})
}

// readRequestBody reads and validates the request body
func readRequestBody(ctx *gin.Context, maxSize int64) ([]byte, error) {
	body := ctx.Request.Body
	defer ctx.Request.Body.Close()

	// Read body bytes
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	// Check if body size exceeds maximum allowed size
	if maxSize > 0 && int64(len(bodyBytes)) > maxSize {
		return nil, fmt.Errorf("request body too large: %d bytes (max: %d bytes)", len(bodyBytes), maxSize)
	}

	return bodyBytes, nil
}
