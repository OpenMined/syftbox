package send

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/utils"
)

var (
	ErrPollTimeout = errors.New("poll timeout")
	ErrNoRequest   = errors.New("no request found")
)

// Config holds the service configuration
type Config struct {
	DefaultTimeoutMs    int
	MaxTimeoutMs        int
	MaxBodySize         int64
	PollIntervalMs      int
	RequestChkTimeoutMs int
}

// SendService handles the business logic for message sending and polling
type SendService struct {
	dispatcher MessageDispatcher
	store      RPCMsgStore
	cfg        *Config
}

// NewSendService creates a new send service
func NewSendService(dispatch MessageDispatcher, store RPCMsgStore, cfg *Config) *SendService {
	if cfg == nil {
		cfg = &Config{
			DefaultTimeoutMs:    1000,    // 1000 ms
			MaxTimeoutMs:        10000,   // 10 seconds
			MaxBodySize:         4 << 20, // 4MB
			PollIntervalMs:      500,     // 500 ms
			RequestChkTimeoutMs: 200,     // 200 ms
		}
	}
	return &SendService{dispatcher: dispatch, store: store, cfg: cfg}
}

// SendMessage handles sending a message to a user
func (s *SendService) SendMessage(ctx context.Context, req *MessageRequest, bodyBytes []byte) (*SendResult, error) {

	// Create the HTTP message

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

	// Dispatch the message to the user via websocket
	if ok := s.dispatcher.Dispatch(req.SyftURL.Datasite, msg); !ok {
		// If the message is not sent via websocket, handle it as an offline message
		return s.handleOfflineMessage(ctx, req, httpMsg)
	}

	// If the message is sent via websocket, handle the response
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

	// Save the RPC message to blob storage
	if err := s.store.StoreMsg(ctx, blobPath, *rpcMsg); err != nil {
		return nil, fmt.Errorf("failed to save message to blob storage: %w", err)
	}

	slog.Info("saved message to blob storage", "blobPath", blobPath)
	return &SendResult{
		Status:    http.StatusAccepted,
		RequestID: httpMsg.Id,
		PollURL:   s.constructPollURL(httpMsg.Id, req.SyftURL, req.From, req.AsRaw),
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
				PollURL:   s.constructPollURL(httpMsg.Id, req.SyftURL, req.From, req.AsRaw),
			}, nil
		}
		return nil, err
	}

	// Read the object
	bodyBytes, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	responseBody, err := unmarshalResponse(bodyBytes, req.AsRaw)
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

	// Validate if the corresponding request exists
	requestBlobPath := path.Join(req.SyftURL.ToLocalPath(), fmt.Sprintf("%s.request", req.RequestID))

	_, err := s.pollForObject(ctx, requestBlobPath, s.cfg.RequestChkTimeoutMs)

	if err != nil {
		if errors.Is(err, ErrPollTimeout) {
			return nil, ErrNoRequest
		}
		return nil, err
	}

	// Check if the corresponding response exists
	responseFileName := fmt.Sprintf("%s.response", req.RequestID)
	responseBlobPath := path.Join(req.SyftURL.ToLocalPath(), responseFileName)

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = s.cfg.DefaultTimeoutMs
	}

	object, err := s.pollForObject(ctx, responseBlobPath, timeout)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	responseBody, err := unmarshalResponse(bodyBytes, req.AsRaw)
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
func (s *SendService) pollForObject(ctx context.Context, blobPath string, timeout int) (io.ReadCloser, error) {
	startTime := time.Now()
	maxTimeout := time.Duration(timeout) * time.Millisecond

	for {
		if time.Since(startTime) > maxTimeout {
			return nil, ErrPollTimeout
		}

		object, err := s.store.GetMsg(ctx, blobPath)
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

	if err := s.store.DeleteMsg(context.Background(), requestPath); err != nil {
		slog.Error("failed to delete request object", "error", err, "path", requestPath)
	}

	if err := s.store.DeleteMsg(context.Background(), responsePath); err != nil {
		slog.Error("failed to delete response object", "error", err, "path", responsePath)
	}
}

// constructPollURL constructs the poll URL for a request
func (s *SendService) constructPollURL(requestID string, syftURL utils.SyftBoxURL, from string, asRaw bool) string {
	return fmt.Sprintf(
		PollURL,
		requestID,
		syftURL.BaseURL(),
		from,
		asRaw,
	)
}

// unmarshalResponse handles the unmarshaling of a response from blob storage
// It expects the response to have a base64 encoded body field that contains JSON
func unmarshalResponse(bodyBytes []byte, asRaw bool) (map[string]interface{}, error) {
	// If the request is raw, return the body as bytes
	if asRaw {
		var bodyJson map[string]interface{}
		err := json.Unmarshal(bodyBytes, &bodyJson)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}
		return map[string]interface{}{"message": bodyJson}, nil
	}

	// Otherwise, unmarshal it as a SyftRPCMessage
	var rpcMsg syftmsg.SyftRPCMessage
	err := json.Unmarshal(bodyBytes, &rpcMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	// decode the body if it is base64 encoded
	// return the SyftRPCMessage as a different json representation
	return map[string]interface{}{"message": rpcMsg.ToJsonMap()}, nil
}

// GetConfig returns the service configuration
func (s *SendService) GetConfig() *Config {
	return s.cfg
}
