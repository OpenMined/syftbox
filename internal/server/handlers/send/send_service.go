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
	ErrPollTimeout     = errors.New("poll timeout")
	ErrRequestNotFound = errors.New("request not found")
)

// Config holds the service configuration
type Config struct {
	DefaultTimeout      time.Duration
	MaxTimeout          time.Duration
	ObjectPollInterval  time.Duration
	RequestCheckTimeout time.Duration
	MaxBodySize         int64
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
			DefaultTimeout:      1 * time.Second,
			MaxTimeout:          10 * time.Second,
			ObjectPollInterval:  200 * time.Millisecond,
			RequestCheckTimeout: 200 * time.Millisecond,
			MaxBodySize:         4 << 20, // 4MB
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

	var timeout time.Duration
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	} else {
		timeout = s.cfg.DefaultTimeout
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

	_, err := s.pollForObject(ctx, requestBlobPath, s.cfg.RequestCheckTimeout)

	if err != nil {
		if errors.Is(err, ErrPollTimeout) {
			return nil, ErrRequestNotFound
		}
		return nil, err
	}

	// Check if the corresponding response exists
	responseFileName := fmt.Sprintf("%s.response", req.RequestID)
	responseBlobPath := path.Join(req.SyftURL.ToLocalPath(), responseFileName)

	var timeout time.Duration
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	} else {
		timeout = s.cfg.DefaultTimeout
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
func (s *SendService) pollForObject(ctx context.Context, blobPath string, timeout time.Duration) (io.ReadCloser, error) {
	startTime := time.Now()

	for {
		if time.Since(startTime) > timeout {
			return nil, ErrPollTimeout
		}

		object, err := s.store.GetMsg(ctx, blobPath)
		if err != nil {
			// if message not found, return immediately - no need to retry
			if errors.Is(err, ErrMsgNotFound) {
				return nil, ErrMsgNotFound
			}
			// For other errors, return immediately as they are likely permanent
			slog.Error("poll for object failed", "error", err, "blobPath", blobPath)
			return nil, err
		} else if object != nil {
			return object, nil
		}

		// Always sleep between polling attempts
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.cfg.ObjectPollInterval):
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
