package send

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// SendHandler handles HTTP requests for sending messages
type SendHandler struct {
	service *SendService
}

// New creates a new send handler
func New(msgDispatcher MessageDispatcher, msgStore RPCMsgStore) *SendHandler {
	service := NewSendService(msgDispatcher, msgStore, nil)
	return &SendHandler{service: service}
}

// SendMsg handles sending a message
func (h *SendHandler) SendMsg(ctx *gin.Context) {
	var req MessageRequest

	// Bind query parameters
	if err := ctx.ShouldBindQuery(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, APIError{
			Error:   ErrorInvalidRequest,
			Message: err.Error(),
		})
		return
	}

	// Bind headers
	if err := ctx.ShouldBindHeader(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, APIError{
			Error:   ErrorInvalidRequest,
			Message: err.Error(),
		})
		return
	}

	// Bind request method
	req.Method = ctx.Request.Method

	// Bind headers
	req.BindHeaders(ctx)

	// Read request body with size limit
	bodyBytes, err := readRequestBody(ctx, h.service.cfg.MaxBodySize)
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, APIError{
			Error:   ErrorInvalidRequest,
			Message: err.Error(),
		})
		return
	}

	result, err := h.service.SendMessage(ctx.Request.Context(), &req, bodyBytes)
	if err != nil {
		slog.Error("failed to send message", "error", err)
		ctx.PureJSON(http.StatusInternalServerError, APIError{
			Error:     ErrorInternal,
			Message:   err.Error(),
			RequestID: result.RequestID,
		})
		return
	}

	if result.Response != nil {
		ctx.PureJSON(result.Status, APIResponse{
			RequestID: result.RequestID,
			Data:      result.Response,
		})
		return
	}

	// add poll url as location header
	ctx.Header("Location", result.PollURL)

	// return poll info
	ctx.PureJSON(result.Status, APIResponse{
		RequestID: result.RequestID,
		Data: PollInfo{
			PollURL: result.PollURL,
		},
		Message: "Request has been accepted. Please check back later.",
	})
}

// PollForResponse handles polling for a response
func (h *SendHandler) PollForResponse(ctx *gin.Context) {
	var req PollObjectRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		slog.Error("failed to bind query parameters", "error", err)
		ctx.PureJSON(http.StatusBadRequest, APIError{
			Error:     ErrorInvalidRequest,
			Message:   err.Error(),
			RequestID: req.RequestID,
		})
		return
	}

	if err := ctx.ShouldBindHeader(&req); err != nil {
		slog.Error("failed to bind headers", "error", err)
		ctx.PureJSON(http.StatusBadRequest, APIError{
			Error:     ErrorInvalidRequest,
			Message:   err.Error(),
			RequestID: req.RequestID,
		})
		return
	}

	result, err := h.service.PollForResponse(ctx.Request.Context(), &req)
	contentTypeHTML := ctx.Request.Header.Get("Content-Type") == "text/html"

	if err != nil {
		if errors.Is(err, ErrPollTimeout) {
			// Add poll URL to the Response header
			pollURL := h.service.constructPollURL(
				req.RequestID,
				req.SyftURL,
				req.From,
				req.AsRaw,
			)

			// calculate refresh interval in seconds
			var refreshInterval int
			if req.Timeout > 0 {
				refreshInterval = req.Timeout / 1000
			} else {
				refreshInterval = h.service.cfg.DefaultTimeoutMs / 1000
			}

			// add poll url as location header and retry after header
			ctx.Header("Location", pollURL)
			ctx.Header("Retry-After", strconv.Itoa(refreshInterval))

			if contentTypeHTML {
				// Return a HTML page with a link to the poll URL
				// with auto refresh capability
				ctx.HTML(http.StatusAccepted, "poll.html", gin.H{
					"PollURL":         pollURL,
					"BaseURL":         ctx.Request.Host,
					"RefreshInterval": refreshInterval, // in seconds
				})
				return
			} else {
				ctx.PureJSON(http.StatusAccepted, APIError{
					Error:     ErrorTimeout,
					Message:   "Polling timeout reached. The request may still be processing.",
					RequestID: req.RequestID,
				})
				return
			}
		}

		if errors.Is(err, ErrNoRequest) {
			ctx.PureJSON(http.StatusNotFound, APIError{
				Error:     ErrorNotFound,
				Message:   "No request found.",
				RequestID: req.RequestID,
			})
			return
		}

		slog.Error("failed to poll for response", "error", err)
		ctx.PureJSON(http.StatusInternalServerError, APIError{
			Error:     ErrorInternal,
			Message:   err.Error(),
			RequestID: req.RequestID,
		})
		return
	}

	ctx.PureJSON(result.Status, APIResponse{
		RequestID: result.RequestID,
		Data:      result.Response,
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
		return nil, fmt.Errorf(
			"request body too large: %d bytes (max: %d bytes)",
			len(bodyBytes),
			maxSize,
		)
	}

	return bodyBytes, nil
}
