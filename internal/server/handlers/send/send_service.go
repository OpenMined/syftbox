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

// SendMessage processes an HTTP request and converts it to an RPC message for delivery.
// It handles both online (WebSocket) and offline (polling) scenarios, with ACL permission
// checking and support for user-partitioned storage via the suffix-sender feature.
func (s *SendService) SendMessage(ctx context.Context, req *MessageRequest, bodyBytes []byte) (*SendResult, error) {

	// If suffix-sender is enabled, append the sender's email to the endpoint path
	// This enables user-partitioned storage: /app/rpc/endpoint/user@domain.com/
	syftURL := req.SyftURL
	if req.SuffixSender {
		syftURL.Endpoint = path.Join(syftURL.Endpoint, req.From)
	}

	// Create the RPC message that will be sent to the target application
	rpcMsg, err := syftmsg.NewSyftRPCMessage(
		req.From,
		req.SyftURL, // Use original URL to keep endpoint unchanged in the RPC message
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

	// Generate ETag for request validation and caching
	etag := fmt.Sprintf("%x", md5.Sum(rpcMsgBytes))

	// Create HTTP message wrapper for WebSocket dispatch
	msg := syftmsg.NewHttpMsg(
		req.From,
		syftURL,
		req.Method,
		rpcMsgBytes,
		req.Headers,
		rpcMsg.ID.String(),
		etag,
	)

	// Build the storage path for the request file
	requestRelPath := path.Join(
		syftURL.ToLocalPath(),
		fmt.Sprintf("%s.%s", rpcMsg.ID.String(), "request"),
	)

	// Verify user has write permission to store request files at this path
	if err := s.checkPermission(requestRelPath, req.From, acl.AccessWrite); err != nil {
		return nil, ErrPermissionDenied
	}

	// Attempt to deliver message immediately via WebSocket if user is online
	dispatched := s.dispatcher.Dispatch(syftURL.Datasite, msg)

	// Persist request in blob storage for offline delivery and polling
	err = s.store.StoreMsg(ctx, requestRelPath, rpcMsgBytes)

	if err != nil {
		return nil, fmt.Errorf("failed to store RPC request: %w", err)
	}

	// If user is offline, return polling URL for async response retrieval
	if !dispatched {
		return &SendResult{
			Status:    http.StatusAccepted,
			RequestID: rpcMsg.ID.String(),
			PollURL: s.constructPollURL(
				rpcMsg.ID.String(),
				req.SyftURL, // Use original URL to maintain consistent polling endpoint
				req.From,
				req.AsRaw,
			),
		}, nil
	}

	// User is online - poll for immediate response
	return s.checkForResponse(ctx, req, rpcMsg)
}

// checkForResponse polls for a response when the user is online.
// It waits for the application to process the request and create a response file.
// Returns the response if found within timeout, otherwise returns a polling URL.
func (s *SendService) checkForResponse(
	ctx context.Context,
	req *MessageRequest,
	rpcMsg *syftmsg.SyftRPCMessage,
) (*SendResult, error) {

	// Apply sender suffix to response path if it was used for the request
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
					req.SyftURL, // Use original URL to maintain consistent polling endpoint
					req.From,
					req.AsRaw,
				),
			}, nil
		}
		return nil, err
	}

	// Read the response file from blob storage
	bodyBytes, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}

	responseBody, err := unmarshalResponse(bodyBytes, req.AsRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Clean up request and response files asynchronously
	go s.cleanReqResponse(syftURL, rpcMsg.ID.String())

	return &SendResult{
		Status:    http.StatusOK,
		RequestID: rpcMsg.ID.String(),
		Response:  responseBody,
	}, nil
}

// PollForResponse retrieves a response for a previously sent request.
// It supports both new (user-partitioned) and legacy (shared) storage formats
// and includes ACL permission checking for security.
func (s *SendService) PollForResponse(ctx context.Context, req *PollObjectRequest) (*PollResult, error) {

	// Find the request file using dual-path resolution (new + legacy)
	findValidRequest := func() (string, bool, error) {

		// Get both new (user-partitioned) and legacy (shared) path candidates
		requestRelPaths := s.getCandidateRequestPaths(req)

		// Try each path until we find an existing request file
		for i, requestRelPath := range requestRelPaths {
			// Check if request file exists at this path
			_, err := s.store.GetMsg(ctx, requestRelPath)
			if err != nil {
				// File not found at this path, try the next candidate
				if errors.Is(err, ErrMsgNotFound) {
					continue
				}
				// File found but error occurred, return the error
				return "", false, err
			}

			// Index 0 = new user-partitioned path, Index 1 = legacy shared path
			withSender := (i == 0)
			return requestRelPath, withSender, nil
		}

		// No request file found in any candidate path
		return "", false, ErrRequestNotFound
	}

	requestRelPath, withSender, err := findValidRequest()
	if err != nil {
		return nil, err
	}

	// Verify user has permission to read the request file
	if err := s.checkPermission(requestRelPath, req.From, acl.AccessRead); err != nil {
		return nil, ErrPermissionDenied
	}

	// Build response file path from request path
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

	// Verify user has permission to read the response file
	if err := s.checkPermission(responseRelPath, req.From, acl.AccessRead); err != nil {
		return nil, ErrPermissionDenied
	}

	responseBody, err := unmarshalResponse(bodyBytes, req.AsRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Apply same sender suffix logic for cleanup path consistency
	syftURL := req.SyftURL
	if withSender {
		syftURL.Endpoint = path.Join(syftURL.Endpoint, req.From)
	}

	// Clean up request and response files asynchronously
	go s.cleanReqResponse(syftURL, req.RequestID)

	return &PollResult{
		Status:    http.StatusOK,
		RequestID: req.RequestID,
		Response:  responseBody,
	}, nil
}

// pollForObject continuously checks blob storage for a file until timeout.
// Returns the file when found, or ErrPollTimeout if not found within timeout.
func (s *SendService) pollForObject(ctx context.Context, blobPath string, timeout time.Duration) (io.ReadCloser, error) {

	// Record start time for timeout calculation
	startTime := time.Now()

	for {
		object, err := s.store.GetMsg(ctx, blobPath)
		if err == nil && object != nil {
			return object, nil
		}
		// Non-"not found" errors are permanent and should be returned immediately
		if err != nil && !errors.Is(err, ErrMsgNotFound) {
			slog.Error("poll for object failed", "error", err, "blobPath", blobPath)
			return nil, err
		}

		if time.Since(startTime) > timeout {
			return nil, ErrPollTimeout
		}

		// Wait before next polling attempt to avoid excessive load
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.cfg.ObjectPollInterval):
			continue
		}
	}
}

// cleanReqResponse removes both request and response files from blob storage.
// This is called after successful response delivery to free up storage space.
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

// constructPollURL builds the polling endpoint URL for async response retrieval.
func (s *SendService) constructPollURL(requestID string, syftURL utils.SyftBoxURL, from string, asRaw bool) string {
	return fmt.Sprintf(
		PollURL,
		requestID,
		syftURL.BaseURL(),
		from,
		asRaw,
	)
}

// unmarshalResponse converts blob storage response data into a JSON map.
// Handles both raw responses and structured SyftRPCMessage responses.
func unmarshalResponse(bodyBytes []byte, asRaw bool) (map[string]interface{}, error) {
	// For raw requests, parse the response body directly as JSON
	if asRaw {
		var bodyJson map[string]interface{}
		err := json.Unmarshal(bodyBytes, &bodyJson)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}
		return map[string]interface{}{"message": bodyJson}, nil
	}

	// For structured requests, parse as SyftRPCMessage and extract the payload
	var rpcMsg syftmsg.SyftRPCMessage

	err := json.Unmarshal(bodyBytes, &rpcMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	// Convert SyftRPCMessage to a standardized JSON format for API response
	return map[string]interface{}{"message": rpcMsg.ToJsonMap()}, nil
}

// GetConfig returns the current service configuration settings.
func (s *SendService) GetConfig() *Config {
	return s.cfg
}

// getCandidateRequestPaths returns both new (user-partitioned) and legacy (shared)
// request file paths for backward compatibility during polling.
func (s *SendService) getCandidateRequestPaths(req *PollObjectRequest) []string {
	filename := fmt.Sprintf("%s.request", req.RequestID)
	basePath := req.SyftURL.ToLocalPath()

	requestPaths := []string{
		// New format: /app/rpc/endpoint/user@domain.com/request-id.request
		path.Join(basePath, req.From, filename),
		// Legacy format: /app/rpc/endpoint/request-id.request
		path.Join(basePath, filename),
	}

	return requestPaths
}

// checkPermission verifies if a user has the required access level to a path.
// Datasite owners have full access, others are checked against ACL rules.
func (s *SendService) checkPermission(path string, user string, level acl.AccessLevel) error {
	// Datasite owners have full access to all files in their datasite
	if datasite.IsOwner(path, user) {
		return nil
	}

	// Non-owners must pass ACL permission checks
	return s.acl.CanAccess(&acl.ACLRequest{
		Path:  path,
		User:  &acl.User{ID: user},
		Level: level,
	})
}
