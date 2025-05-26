package send

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
)

var (
	defaultTimeoutMs = 1000
	maxTimeoutMs     = 10000
	maxBodySize      = 4 << 20 // 4Mb
	ErrPollTimeout   = errors.New("poll timed out")
)

type SendHandler struct {
	hub  *ws.WebsocketHub
	blob *blob.BlobService
}

func New(hub *ws.WebsocketHub, blob *blob.BlobService) *SendHandler {
	return &SendHandler{hub: hub, blob: blob}
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

func (h *SendHandler) SendMsg(ctx *gin.Context) {
	var req MessageRequest
	if err := ctx.ShouldBindHeader(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Read request body with size limit (4MB)
	bodyBytes, err := readRequestBody(ctx, int64(maxBodySize))
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	msg := syftmsg.NewHttpMsg(
		req.From,
		req.To,
		req.AppName,
		req.AppEp,
		ctx.Request.Method,
		bodyBytes,
		req.Headers,
		req.Status,
		syftmsg.HttpMsgTypeRequest,
	)
	slog.Debug("sending message", "msg", msg)

	// httpMsg
	httpMsg := msg.Data.(*syftmsg.HttpMsg)

	//  send the message to the websocket hub
	if ok := h.hub.SendMessageUser(req.To, msg); !ok {
		// Handle saving to blob storage
		// get the blob path
		blobPath := path.Join(
			req.To,
			"app_data",
			req.AppName,
			"rpc",
			req.AppEp,
			fmt.Sprintf("%s.%s", httpMsg.Id, httpMsg.Type),
		)

		// convert httpMsg to RPCMsg
		rpcMsg := syftmsg.NewSyftRPCMessage(*httpMsg)

		rpcMsgBytes, err := json.Marshal(rpcMsg)
		if err != nil {
			slog.Error("failed to marshal RPCMsg", "error", err)
			ctx.PureJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if _, err := h.blob.Backend().PutObject(context.Background(), &blob.PutObjectParams{
			Key:  blobPath,
			ETag: rpcMsg.ID.String(),
			Body: bytes.NewReader(rpcMsgBytes),
			Size: int64(len(rpcMsgBytes)),
		}); err != nil {
			slog.Error("failed to save message to blob storage", "error", err)
			ctx.PureJSON(http.StatusInternalServerError, SendError{
				Error:     err.Error(),
				RequestID: httpMsg.Id,
			})
			return
		}
		slog.Info("saved message to blob storage", "blobPath", blobPath)
		ctx.PureJSON(http.StatusAccepted, SendAcknowledgment{
			Message:   "Request has been accepted",
			RequestID: httpMsg.Id,
			PollURL:   h.constructPollURL(httpMsg.Id, req.To, req.AppName, req.AppEp),
		})
		return
	}

	// create blobPath
	blobPath := path.Join(
		req.To,
		"app_data",
		req.AppName,
		"rpc",
		req.AppEp,
		fmt.Sprintf("%s.response", httpMsg.Id),
	)

	// poll for the object
	var object *blob.GetObjectResponse

	// Ensure a valid timeout value
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeoutMs
	}

	object, err = h.pollForObject(context.Background(), blobPath, timeout)
	if err != nil && err != ErrPollTimeout {
		ctx.PureJSON(http.StatusInternalServerError, SendError{
			Error:     err.Error(),
			RequestID: httpMsg.Id,
		})
		return
	}

	// if the object is not found, return an accepted message
	if object == nil {
		ctx.PureJSON(http.StatusAccepted, SendAcknowledgment{
			Message:   "Request has been accepted. Please check back later.",
			RequestID: httpMsg.Id,
			PollURL:   h.constructPollURL(httpMsg.Id, req.To, req.AppName, req.AppEp),
		})
		return
	}

	// read the object body
	bodyBytes, readErr := io.ReadAll(object.Body)
	if readErr != nil {
		ctx.PureJSON(
			http.StatusInternalServerError,
			SendError{
				Error:     "Failed to read object: " + readErr.Error(),
				RequestID: httpMsg.Id,
			},
		)
		return
	}

	// unmarshal the json object
	responseBody, unmarshalErr := unmarshalResponse(bodyBytes)
	if unmarshalErr != nil {
		ctx.PureJSON(
			http.StatusInternalServerError,
			SendError{
				Error:     "Failed to unmarshal response: " + unmarshalErr.Error(),
				RequestID: httpMsg.Id,
			},
		)
		return
	}

	// send the response to the client
	ctx.PureJSON(http.StatusOK, PollResponse{
		Message:   responseBody,
		RequestID: httpMsg.Id,
	})

}

func (h *SendHandler) PollForResponse(ctx *gin.Context) {
	var req PollObjectRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		slog.Error("failed to bind query parameters", "error", err)
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// create the blob path to the app endpoint
	fileName := fmt.Sprintf("%s.response", req.RequestID)

	// create the blob path to the app endpoint
	blobPath := path.Join(req.User, "app_data", req.AppName, "rpc", req.AppEp, fileName)

	slog.Info("polling for response", "blobPath", blobPath)

	// poll the blob storage for the response
	var queryTimeout int
	if req.Timeout > 0 {
		queryTimeout = req.Timeout
	} else {
		queryTimeout = defaultTimeoutMs
	}

	object, err := h.pollForObject(context.Background(), blobPath, queryTimeout)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, PollError{
			Error:     err.Error(),
			RequestID: req.RequestID,
		})
		return
	}

	// Read the response body
	bodyBytes, err := io.ReadAll(object.Body)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, PollError{
			Error:     err.Error(),
			RequestID: req.RequestID,
		})
		return
	}

	// Unmarshal the response
	responseBody, err := unmarshalResponse(bodyBytes)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, PollError{
			Error:     err.Error(),
			RequestID: req.RequestID,
		})
		return
	}

	// send the response to the client
	ctx.PureJSON(
		http.StatusOK,
		PollResponse{
			Message:   responseBody,
			RequestID: req.RequestID,
		},
	)
}

func (h *SendHandler) pollForObject(
	ctx context.Context,
	blobPath string,
	timeout int,
) (*blob.GetObjectResponse, error) {
	startTime := time.Now()

	// clamp timeout
	if timeout > maxTimeoutMs {
		timeout = maxTimeoutMs
	}

	maxTimeout := time.Duration(timeout) * time.Millisecond

	for {
		// check if timeout has been reached
		if time.Since(startTime) > time.Duration(maxTimeout) {
			return nil, ErrPollTimeout
		}

		// try to get the object
		// Attempt to get the object and handle any errors
		object, err := h.blob.Backend().GetObject(ctx, blobPath)
		if err != nil {
			// Log the error for visibility
			slog.Error("Failed to get object from backend", "error", err, "blobPath", blobPath)
			// Optionally, decide whether to break the loop for non-recoverable errors
			// For now, we continue retrying
			continue
		}
		if object != nil {
			return object, nil
		}

		// if we get here, there was an error but we haven't timed out yet
		// wait a bit before trying again
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond): // adjust polling interval as needed
			continue
		}
	}
}

func (h *SendHandler) constructPollURL(
	requestID string,
	user string,
	appName string,
	appEp string,
) string {
	return fmt.Sprintf(
		"/send/poll?request_id=%s&user=%s&app_name=%s&app_endpoint=%s",
		requestID,
		user,
		appName,
		appEp,
	)
}

// unmarshalResponse handles the unmarshaling of a response from blob storage
// It expects the response to have a base64 encoded body field that contains JSON
func unmarshalResponse(bodyBytes []byte) (map[string]interface{}, error) {
	// First unmarshal the outer response
	var response map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Get the base64 encoded body
	bodyStr, ok := response["body"].(string)
	if !ok {
		return nil, fmt.Errorf("body field is not a string")
	}

	// Decode the base64 body
	decodedBody, err := base64.URLEncoding.DecodeString(bodyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 body: %w", err)
	}

	// Unmarshal the decoded body as JSON
	var responseBody map[string]interface{}
	if err := json.Unmarshal(decodedBody, &responseBody); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decoded body: %w", err)
	}

	// TODO: Clean up response and request files

	return responseBody, nil
}
