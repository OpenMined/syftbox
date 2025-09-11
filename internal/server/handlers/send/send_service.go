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
	"strings"
	"time"

	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/utils"
)

var (
	ErrPollTimeout      = errors.New("poll timeout")
	ErrRequestNotFound  = errors.New("request not found")
	ErrPermissionDenied = errors.New("permission denied")
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
	acl        *acl.ACLService
}

// NewSendService creates a new send service
func NewSendService(dispatch MessageDispatcher, store RPCMsgStore, acl *acl.ACLService, cfg *Config) *SendService {
	if cfg == nil {
		cfg = &Config{
			DefaultTimeout:      1 * time.Second,
			MaxTimeout:          10 * time.Second,
			ObjectPollInterval:  200 * time.Millisecond,
			RequestCheckTimeout: 200 * time.Millisecond,
			MaxBodySize:         4 << 20, // 4MB
		}
	}
	return &SendService{dispatcher: dispatch, store: store, acl: acl, cfg: cfg}
}

// SendMessage handles sending a message to a user
func (s *SendService) SendMessage(ctx context.Context, req *MessageRequest, bodyBytes []byte) (*SendResult, error) {

	// if the sender suffix is enabled, add the sender to the endpoint
	syftURL := req.SyftURL
	if req.SuffixSender {
		syftURL.Endpoint = path.Join(syftURL.Endpoint, req.From)
	}

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

	// create an etag for the request
	etag := fmt.Sprintf("%x", md5.Sum(rpcMsgBytes))

	// create a new HTTP message
	msg := syftmsg.NewHttpMsg(
		req.From,
		syftURL,
		req.Method,
		rpcMsgBytes,
		req.Headers,
		rpcMsg.ID.String(),
		etag,
	)

	// Relative path to the request file
	requestRelPath := path.Join(
		syftURL.ToLocalPath(),
		fmt.Sprintf("%s.%s", rpcMsg.ID.String(), "request"),
	)

	// Check if the user has permission to send message to this application
	if err := s.checkPermission(requestRelPath, req.From, acl.AccessWrite); err != nil {
		return nil, ErrPermissionDenied
	}

	// Dispatch the message to the user via websocket
	dispatched := s.dispatcher.Dispatch(syftURL.Datasite, msg)

	// Store the request in blob storage
	err = s.store.StoreMsg(ctx, requestRelPath, rpcMsgBytes)

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
// it polls for the response in blob storage until the timeout is reached
// if the response is found, it returns the response
// if the timeout is reached, it returns an error
func (s *SendService) checkForResponse(
	ctx context.Context,
	req *MessageRequest,
	rpcMsg *syftmsg.SyftRPCMessage,
) (*SendResult, error) {

	syftURL := rpcMsg.URL
	if req.SuffixSender {
		syftURL.Endpoint = path.Join(syftURL.Endpoint, req.From)
	}

	responseRelPath := path.Join(
		syftURL.ToLocalPath(),
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
				PollURL: s.constructPollURL(
					rpcMsg.ID.String(),
					req.SyftURL,
					req.From,
					req.AsRaw,
				),
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
	go s.cleanReqResponse(syftURL, rpcMsg.ID.String())

	return &SendResult{
		Status:    http.StatusOK,
		RequestID: rpcMsg.ID.String(),
		Response:  responseBody,
	}, nil
}

// PollForResponse handles polling for a response
func (s *SendService) PollForResponse(ctx context.Context, req *PollObjectRequest) (*PollResult, error) {

	// Validate if the corresponding request exists
	findValidRequest := func() (string, bool, error) {

		// Get the candidate request paths
		requestRelPaths := s.getCandidateRequestPaths(req)

		// Check if the request exists in the candidate paths
		for i, requestRelPath := range requestRelPaths {
			// Get the request from the blob storage
			_, err := s.store.GetMsg(ctx, requestRelPath)
			if err != nil {
				// If the request is not found, continue to the next path
				if errors.Is(err, ErrMsgNotFound) {
					continue
				}
				// If the request is found, return nil
				return "", false, err
			}

			// i == 0 means withSender (new path),
			// i == 1 means withoutSender (legacy path)
			withSender := (i == 0)
			return requestRelPath, withSender, nil
		}

		// If the request is not found in any of the candidate paths, return an error
		return "", false, ErrRequestNotFound
	}

	requestRelPath, withSender, err := findValidRequest()
	if err != nil {
		return nil, err
	}

	// Check if user has read access to the request
	if err := s.checkPermission(requestRelPath, req.From, acl.AccessRead); err != nil {
		return nil, ErrPermissionDenied
	}

	// Check if the corresponding response exists
	responseRelPath := strings.Replace(requestRelPath, ".request", ".response", 1)

	var timeout time.Duration
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	} else {
		timeout = s.cfg.DefaultTimeout
	}

	object, err := s.pollForObject(ctx, responseRelPath, timeout)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	// Check if user has read access to the response
	if err := s.checkPermission(responseRelPath, req.From, acl.AccessRead); err != nil {
		return nil, ErrPermissionDenied
	}

	responseBody, err := unmarshalResponse(bodyBytes, req.AsRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check if sender suffix needs to be added to the request path
	syftURL := req.SyftURL
	if withSender {
		syftURL.Endpoint = path.Join(syftURL.Endpoint, req.From)
	}

	// Clean up in background
	go s.cleanReqResponse(syftURL, req.RequestID)

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
func (s *SendService) cleanReqResponse(syftURL utils.SyftBoxURL, requestID string) {
	requestPath := path.Join(syftURL.ToLocalPath(), fmt.Sprintf("%s.request", requestID))
	responsePath := path.Join(syftURL.ToLocalPath(), fmt.Sprintf("%s.response", requestID))

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

// Returns both new and legacy request paths
func (s *SendService) getCandidateRequestPaths(req *PollObjectRequest) []string {
	filename := fmt.Sprintf("%s.request", req.RequestID)
	basePath := req.SyftURL.ToLocalPath()

	requestPaths := []string{
		// Try sender suffix path first (new request path)
		path.Join(basePath, req.From, filename),
		// Fallback to legacy path (old request path)
		path.Join(basePath, filename),
	}

	return requestPaths
}

// Check permission to the path
func (s *SendService) checkPermission(path string, user string, level acl.AccessLevel) error {
	// if the user is the owner of the datasite, they have access
	if datasite.IsOwner(path, user) {
		return nil
	}

	// Otherwise, check if the user has access to the path
	return s.acl.CanAccess(&acl.ACLRequest{
		Path:  path,
		User:  &acl.User{ID: user},
		Level: level,
	})
}
