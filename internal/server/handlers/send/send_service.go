package send

import (
	"context"
	"crypto/md5"
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

	// create an RPC message
	rpcMsg, err := syftmsg.NewSyftRPCMessage(
		req.From,
		req.SyftURL,
		syftmsg.SyftMethod(req.Method),
		bodyBytes,
		req.Headers,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPC message: %w", err)
	}

	rpcMsgBytes, err := rpcMsg.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RPC message: %w", err)
	}

	etag := fmt.Sprintf("%x", md5.Sum(rpcMsgBytes))

	msg := syftmsg.NewHttpMsg(
		req.From,
		req.SyftURL,
		req.Method,
		rpcMsgBytes,
		req.Headers,
		rpcMsg.ID.String(),
		etag,
	)

	// TODO: Check if user has permission to send message to this application

	// Dispatch the message to the user via websocket
	dispatched := s.dispatcher.Dispatch(req.SyftURL.Datasite, msg)

	// relative rpc request file path to datasite
	relPath := path.Join(
		req.SyftURL.ToLocalPath(),
		fmt.Sprintf("%s.%s", rpcMsg.ID.String(), "request"),
	)

	// Store the request in blob storage
	err = s.store.StoreMsg(ctx, relPath, rpcMsgBytes)

	if err != nil {
		return nil, fmt.Errorf("failed to store RPC request: %w", err)
	}

	// If the message is not dispatched via websocket, return the poll url
	if !dispatched {
		return &SendResult{
			Status:    http.StatusAccepted,
			RequestID: rpcMsg.ID.String(),
			PollURL:   s.constructPollURL(rpcMsg.ID.String(), req.SyftURL, req.From, req.AsRaw),
		}, nil
	}

	// If the message is sent via websocket, handle the response
	return s.checkForResponse(ctx, req, rpcMsg)
}

// checkForResponse handles sending a message when the user is online
func (s *SendService) checkForResponse(
	ctx context.Context,
	req *MessageRequest,
	rpcMsg *syftmsg.SyftRPCMessage,
) (*SendResult, error) {
	responseRelPath := path.Join(
		rpcMsg.URL.ToLocalPath(),
		fmt.Sprintf("%s.response", rpcMsg.ID.String()),
	)

	var timeout time.Duration
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	} else {
		timeout = s.cfg.DefaultTimeout
	}

	object, err := s.pollForObject(ctx, responseRelPath, timeout)
	if err != nil {
		if errors.Is(err, ErrPollTimeout) {
			return &SendResult{
				Status:    http.StatusAccepted,
				RequestID: rpcMsg.ID.String(),
				PollURL:   s.constructPollURL(rpcMsg.ID.String(), req.SyftURL, req.From, req.AsRaw),
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
		rpcMsg.ID.String(),
	)

	return &SendResult{
		Status:    http.StatusOK,
		RequestID: rpcMsg.ID.String(),
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

// pollForObject polls for an object in blob storage until the timeout is reached
// if the object is found, it returns the object
func (s *SendService) pollForObject(ctx context.Context, blobPath string, timeout time.Duration) (io.ReadCloser, error) {

	// start the timer
	startTime := time.Now()

	for {
		object, err := s.store.GetMsg(ctx, blobPath)
		if err == nil && object != nil {
			return object, nil
		}
		// If the error is not "not found", log and return immediately (permanent error)
		if err != nil && !errors.Is(err, ErrMsgNotFound) {
			slog.Error("poll for object failed", "error", err, "blobPath", blobPath)
			return nil, err
		}

		if time.Since(startTime) > timeout {
			return nil, ErrPollTimeout
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
