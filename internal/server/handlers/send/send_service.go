package send

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/utils"
)

var (
	ErrPollTimeout = errors.New("poll timeout")
)

// SendService handles the business logic for message sending and polling
type SendService struct {
	hub  *ws.WebsocketHub
	blob *blob.BlobService
	cfg  *Config
}

// Config holds the service configuration
type Config struct {
	DefaultTimeoutMs int
	MaxTimeoutMs     int
	MaxBodySize      int64
	PollIntervalMs   int
}

// NewSendService creates a new send service
func NewSendService(hub *ws.WebsocketHub, blob *blob.BlobService, cfg *Config) *SendService {
	if cfg == nil {
		cfg = &Config{
			DefaultTimeoutMs: 200,     // 200 ms
			MaxTimeoutMs:     10000,   // 10 seconds
			MaxBodySize:      4 << 20, // 4MB
			PollIntervalMs:   500,     // 500 ms
		}
	}
	return &SendService{hub: hub, blob: blob, cfg: cfg}
}

// SendMessage handles sending a message to a user
func (s *SendService) SendMessage(ctx context.Context, req *MessageRequest, bodyBytes []byte) (*SendResult, error) {
	msg := syftmsg.NewHttpMsg(
		req.From,
		req.SyftURL,
		req.Method,
		bodyBytes,
		req.Headers,
		syftmsg.HttpMsgTypeRequest,
	)

	httpMsg := msg.Data.(*syftmsg.HttpMsg)

	// TODO: Check if user has permission to send message to this application

	// Try sending via websocket first
	if ok := s.hub.SendMessageUser(req.SyftURL.Datasite, msg); !ok {
		return s.handleOfflineMessage(ctx, req, httpMsg)
	}

	return s.handleOnlineMessage(ctx, req, httpMsg)
}

// handleOfflineMessage handles sending a message when the user is offline
func (s *SendService) handleOfflineMessage(
	ctx context.Context,
	req *MessageRequest,
	httpMsg *syftmsg.HttpMsg,
) (*SendResult, error) {
	blobPath := path.Join(
		req.SyftURL.ToLocalPath(),
		fmt.Sprintf("%s.%s", httpMsg.Id, httpMsg.Type),
	)

	// Create the RPC message
	rpcMsg, err := syftmsg.NewSyftRPCMessage(*httpMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPCMsg: %w", err)
	}

	// Marshal the RPC message
	rpcMsgBytes, err := json.Marshal(rpcMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RPCMsg: %w", err)
	}

	// Save the RPC message to blob storage
	if _, err := s.blob.Backend().PutObject(ctx, &blob.PutObjectParams{
		Key:  blobPath,
		ETag: rpcMsg.ID.String(),
		Body: bytes.NewReader(rpcMsgBytes),
		Size: int64(len(rpcMsgBytes)),
	}); err != nil {
		return nil, fmt.Errorf("failed to save message to blob storage: %w", err)
	}

	slog.Info("saved message to blob storage", "blobPath", blobPath)
	return &SendResult{
		Status:    http.StatusAccepted,
		RequestID: httpMsg.Id,
		PollURL:   s.constructPollURL(httpMsg.Id, req.SyftURL),
	}, nil
}

// handleOnlineMessage handles sending a message when the user is online
func (s *SendService) handleOnlineMessage(
	ctx context.Context,
	req *MessageRequest,
	httpMsg *syftmsg.HttpMsg,
) (*SendResult, error) {
	blobPath := path.Join(
		req.SyftURL.ToLocalPath(),
		fmt.Sprintf("%s.response", httpMsg.Id),
	)

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = s.cfg.DefaultTimeoutMs
	}

	object, err := s.pollForObject(ctx, blobPath, timeout)
	if err != nil {
		if errors.Is(err, ErrPollTimeout) {
			return &SendResult{
				Status:    http.StatusAccepted,
				RequestID: httpMsg.Id,
				PollURL:   s.constructPollURL(httpMsg.Id, req.SyftURL),
			}, nil
		}
		return nil, err
	}

	bodyBytes, err := io.ReadAll(object.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	responseBody, err := unmarshalResponse(bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Clean up in background
	go s.cleanReqResponse(
		req.SyftURL.Datasite,
		req.SyftURL.AppName,
		req.SyftURL.Endpoint,
		httpMsg.Id,
	)

	return &SendResult{
		Status:    http.StatusOK,
		RequestID: httpMsg.Id,
		Response:  responseBody,
	}, nil
}

// PollForResponse handles polling for a response
func (s *SendService) PollForResponse(ctx context.Context, req *PollObjectRequest) (*PollResult, error) {
	fileName := fmt.Sprintf("%s.response", req.RequestID)
	blobPath := path.Join(req.SyftURL.ToLocalPath(), fileName)

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = s.cfg.DefaultTimeoutMs
	}

	object, err := s.pollForObject(ctx, blobPath, timeout)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(object.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	responseBody, err := unmarshalResponse(bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Clean up in background
	go s.cleanReqResponse(
		req.SyftURL.Datasite,
		req.SyftURL.AppName,
		req.SyftURL.Endpoint,
		req.RequestID,
	)

	return &PollResult{
		Status:    http.StatusOK,
		RequestID: req.RequestID,
		Response:  responseBody,
	}, nil
}

// pollForObject polls for an object in blob storage
func (s *SendService) pollForObject(ctx context.Context, blobPath string, timeout int) (*blob.GetObjectResponse, error) {
	startTime := time.Now()
	maxTimeout := time.Duration(timeout) * time.Millisecond

	for {
		if time.Since(startTime) > maxTimeout {
			return nil, ErrPollTimeout
		}

		object, err := s.blob.Backend().GetObject(ctx, blobPath)
		if err != nil {
			slog.Error("Failed to get object from backend", "error", err, "blobPath", blobPath)
			continue
		}
		if object != nil {
			return object, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(s.cfg.PollIntervalMs) * time.Millisecond):
			continue
		}
	}
}

// cleanReqResponse cleans up request and response files
func (s *SendService) cleanReqResponse(sender, appName, appEp, requestID string) {
	requestPath := path.Join(sender, "app_data", appName, "rpc", appEp, fmt.Sprintf("%s.request", requestID))
	responsePath := path.Join(sender, "app_data", appName, "rpc", appEp, fmt.Sprintf("%s.response", requestID))

	if _, err := s.blob.Backend().DeleteObject(context.Background(), requestPath); err != nil {
		slog.Error("failed to delete request object", "error", err, "path", requestPath)
	}

	if _, err := s.blob.Backend().DeleteObject(context.Background(), responsePath); err != nil {
		slog.Error("failed to delete response object", "error", err, "path", responsePath)
	}
}

// constructPollURL constructs the poll URL for a request
func (s *SendService) constructPollURL(requestID string, syftURL utils.SyftBoxURL) string {
	return fmt.Sprintf(
		PollURL,
		requestID,
		syftURL.BaseURL(),
	)
}

// unmarshalResponse handles the unmarshaling of a response from blob storage
// It expects the response to have a base64 encoded body field that contains JSON
func unmarshalResponse(bodyBytes []byte) (map[string]interface{}, error) {
	// First unmarshal the outer response
	var rpcMsg syftmsg.SyftRPCMessage
	err := json.Unmarshal(bodyBytes, &rpcMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// return the SyftRPCMessage as a different json representation
	return map[string]interface{}{"message": rpcMsg.ToJsonMap()}, nil
}
