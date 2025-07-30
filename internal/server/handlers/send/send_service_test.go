package send

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMessageDispatcher implements MessageDispatcher for testing
type MockMessageDispatcher struct {
	mock.Mock
}

func (m *MockMessageDispatcher) Dispatch(datasite string, msg *syftmsg.Message) bool {
	args := m.Called(datasite, msg)
	return args.Bool(0)
}

// MockRPCMsgStore implements RPCMsgStore for testing
type MockRPCMsgStore struct {
	mock.Mock
}

func (m *MockRPCMsgStore) StoreMsg(ctx context.Context, path string, msgBytes []byte) error {
	args := m.Called(ctx, path, msgBytes)
	return args.Error(0)
}

func (m *MockRPCMsgStore) GetMsg(ctx context.Context, path string) (io.ReadCloser, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockRPCMsgStore) DeleteMsg(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

func TestNewSendService(t *testing.T) {
	// Test with custom config
	dispatcher := &MockMessageDispatcher{}
	store := &MockRPCMsgStore{}
	cfg := &Config{
		DefaultTimeout:      2 * time.Second,
		MaxTimeout:          20 * time.Second,
		MaxBodySize:         8 << 20,
		ObjectPollInterval:  1 * time.Second,
		RequestCheckTimeout: 400 * time.Millisecond,
	}

	service := NewSendService(dispatcher, store, cfg)
	assert.NotNil(t, service)
	assert.Equal(t, cfg, service.cfg)

	// Test with nil config (should use defaults)
	service = NewSendService(dispatcher, store, nil)
	assert.NotNil(t, service)
	assert.NotNil(t, service.cfg)
	assert.Equal(t, 1*time.Second, service.cfg.DefaultTimeout)
	assert.Equal(t, 10*time.Second, service.cfg.MaxTimeout)
	assert.Equal(t, int64(4<<20), service.cfg.MaxBodySize)
	assert.Equal(t, 200*time.Millisecond, service.cfg.ObjectPollInterval)
	assert.Equal(t, 200*time.Millisecond, service.cfg.RequestCheckTimeout)
}

func TestSendService_SendMessage_Online(t *testing.T) {
	dispatcher := &MockMessageDispatcher{}
	store := &MockRPCMsgStore{}
	service := NewSendService(dispatcher, store, nil)

	// Create test data
	from := "test-user"
	syftURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/testapp/rpc/endpoint")
	assert.NoError(t, err)
	method := "POST"
	body := []byte(`{"key": "value"}`)
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	// Create request
	req := &MessageRequest{
		From:    from,
		SyftURL: *syftURL,
		Method:  method,
		Headers: headers,
	}

	// Set up mock expectations
	// The dispatcher will be called with an HttpMsg containing the marshaled RPC message
	dispatcher.On("Dispatch", syftURL.Datasite, mock.MatchedBy(func(msg *syftmsg.Message) bool {
		httpMsg, ok := msg.Data.(*syftmsg.HttpMsg)
		if !ok {
			return false
		}
		// The body should be a marshaled RPC message, not the original body
		// We'll check basic properties
		return httpMsg.From == from &&
			httpMsg.Method == method &&
			len(httpMsg.Body) > 0 && // Should have RPC message bytes
			httpMsg.Id != "" && // Should have UUID from RPC message
			httpMsg.Etag != "" // Should have etag
	})).Return(true)

	// Mock StoreMsg for storing the request
	store.On("StoreMsg", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Create response message
	responseMsg := &syftmsg.SyftRPCMessage{
		ID:         uuid.New(),
		Sender:     "test-datasite",
		URL:        *syftURL,
		Body:       []byte(`{"response": "success"}`),
		Headers:    headers,
		Created:    time.Now().UTC(),
		Expires:    time.Now().UTC().Add(24 * time.Hour),
		Method:     syftmsg.MethodPOST,
		StatusCode: syftmsg.StatusOK,
	}

	// Mock GetMsg to return the response
	responseBytes, err := json.Marshal(responseMsg)
	assert.NoError(t, err)
	store.On("GetMsg", mock.Anything, mock.Anything).Return(io.NopCloser(bytes.NewReader(responseBytes)), nil)

	// Set up expectations for DeleteMsg calls in cleanReqResponse
	wg := &sync.WaitGroup{}
	wg.Add(2)

	store.On("DeleteMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".request")
	})).
		Run(func(args mock.Arguments) {
			wg.Done()
		}).
		Return(nil)

	store.On("DeleteMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".response")
	})).
		Run(func(args mock.Arguments) {
			wg.Done()
		}).
		Return(nil)

	// Call SendMessage
	result, err := service.SendMessage(context.Background(), req, body)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, http.StatusOK, result.Status)
	assert.NotEmpty(t, result.RequestID)
	assert.NotNil(t, result.Response)

	// Wait for cleanup goroutine to complete
	// Use a reasonable timeout to prevent test hanging
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Cleanup completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("Cleanup goroutine timed out")
	}

	// Verify mock expectations
	dispatcher.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestSendService_SendMessage_Offline(t *testing.T) {
	dispatcher := &MockMessageDispatcher{}
	store := &MockRPCMsgStore{}
	service := NewSendService(dispatcher, store, nil)

	// Create test data
	from := "test-user"
	syftURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/testapp/rpc/endpoint")
	assert.NoError(t, err)

	method := "POST"
	body := []byte(`{"key": "value"}`)
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	// Create request
	req := &MessageRequest{
		From:    from,
		SyftURL: *syftURL,
		Method:  method,
		Headers: headers,
	}

	// Set up mock expectations
	// The dispatcher will be called with an HttpMsg containing the marshaled RPC message
	dispatcher.On("Dispatch", syftURL.Datasite, mock.MatchedBy(func(msg *syftmsg.Message) bool {
		httpMsg, ok := msg.Data.(*syftmsg.HttpMsg)
		if !ok {
			return false
		}
		// The body should be a marshaled RPC message, not the original body
		return httpMsg.From == from &&
			httpMsg.Method == method &&
			len(httpMsg.Body) > 0 && // Should have RPC message bytes
			httpMsg.Id != "" && // Should have UUID from RPC message
			httpMsg.Etag != "" // Should have etag
	})).Return(false) // Return false to simulate offline user
	store.On("StoreMsg", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Call SendMessage
	result, err := service.SendMessage(context.Background(), req, body)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, http.StatusAccepted, result.Status)
	assert.NotEmpty(t, result.RequestID)
	assert.NotEmpty(t, result.PollURL)

	// Verify mock expectations
	dispatcher.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestSendService_PollForResponse(t *testing.T) {
	dispatcher := &MockMessageDispatcher{}
	store := &MockRPCMsgStore{}
	service := NewSendService(dispatcher, store, nil)

	// Create test data
	requestID := uuid.New().String()
	from := "test-user"
	syftURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/testapp/rpc/endpoint")
	assert.NoError(t, err)

	// Create request
	req := &PollObjectRequest{
		RequestID: requestID,
		From:      from,
		SyftURL:   *syftURL,
	}

	// Create response message
	responseMsg := &syftmsg.SyftRPCMessage{
		ID:         uuid.New(),
		Sender:     "test-datasite",
		URL:        *syftURL,
		Body:       []byte(`{"response": "success"}`),
		Headers:    map[string]string{"Content-Type": "application/json"},
		Created:    time.Now().UTC(),
		Expires:    time.Now().UTC().Add(24 * time.Hour),
		Method:     syftmsg.MethodPOST,
		StatusCode: syftmsg.StatusOK,
	}

	// Mock GetMsg to return both request and response
	requestBytes, err := json.Marshal(responseMsg)
	assert.NoError(t, err)
	responseBytes, err := json.Marshal(responseMsg)
	assert.NoError(t, err)

	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return path == syftURL.ToLocalPath()+"/"+requestID+".request"
	})).Return(io.NopCloser(bytes.NewReader(requestBytes)), nil)
	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return path == syftURL.ToLocalPath()+"/"+requestID+".response"
	})).Return(io.NopCloser(bytes.NewReader(responseBytes)), nil)

	// Set up expectations for DeleteMsg calls in cleanReqResponse
	wg := &sync.WaitGroup{}
	wg.Add(2)

	store.On("DeleteMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".request")
	})).
		Run(func(args mock.Arguments) {
			wg.Done()
		}).
		Return(nil)

	store.On("DeleteMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".response")
	})).
		Run(func(args mock.Arguments) {
			wg.Done()
		}).
		Return(nil)

	// Call PollForResponse
	result, err := service.PollForResponse(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, http.StatusOK, result.Status)
	assert.Equal(t, requestID, result.RequestID)
	assert.NotNil(t, result.Response)

	// Wait for cleanup goroutine to complete
	// Use a reasonable timeout to prevent test hanging
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Cleanup completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("Cleanup goroutine timed out")
	}

	// Verify mock expectations
	store.AssertExpectations(t)
}

func TestSendService_PollForResponse_NoRequest(t *testing.T) {
	dispatcher := &MockMessageDispatcher{}
	store := &MockRPCMsgStore{}
	service := NewSendService(dispatcher, store, nil)

	// Create test data
	requestID := uuid.New().String()
	from := "test-user"
	syftURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/testapp/rpc/endpoint")
	assert.NoError(t, err)

	// Create request
	req := &PollObjectRequest{
		RequestID: requestID,
		From:      from,
		SyftURL:   *syftURL,
	}

	// Mock GetMsg to return ErrMsgNotFound for request
	store.On("GetMsg", mock.Anything, mock.Anything).Return(nil, ErrMsgNotFound)

	// Call PollForResponse
	result, err := service.PollForResponse(context.Background(), req)
	assert.Error(t, err)
	assert.Equal(t, ErrRequestNotFound, err)
	assert.Nil(t, result)

	// Verify mock expectations
	store.AssertExpectations(t)
}

func TestSendService_PollForResponse_Timeout(t *testing.T) {
	dispatcher := &MockMessageDispatcher{}
	store := &MockRPCMsgStore{}
	cfg := &Config{
		DefaultTimeout:      100 * time.Millisecond,
		MaxTimeout:          1000 * time.Millisecond,
		MaxBodySize:         4 << 20,
		ObjectPollInterval:  50 * time.Millisecond,
		RequestCheckTimeout: 50 * time.Millisecond,
	}
	service := NewSendService(dispatcher, store, cfg)

	// Create test data
	requestID := uuid.New().String()
	from := "test-user"
	syftURL, err := utils.FromSyftURL("syft://test@datasite.com/app_data/testapp/rpc/endpoint")
	assert.NoError(t, err)

	// Create request
	req := &PollObjectRequest{
		RequestID: requestID,
		From:      from,
		SyftURL:   *syftURL,
		Timeout:   100, // 100 milliseconds
	}

	// Create response message for request check
	responseMsg := &syftmsg.SyftRPCMessage{
		ID:         uuid.New(),
		Sender:     "test-datasite",
		URL:        *syftURL,
		Body:       []byte(`{"response": "success"}`),
		Headers:    map[string]string{"Content-Type": "application/json"},
		Created:    time.Now().UTC(),
		Expires:    time.Now().UTC().Add(24 * time.Hour),
		Method:     syftmsg.MethodPOST,
		StatusCode: syftmsg.StatusOK,
	}

	// Mock GetMsg to return request but timeout for response
	requestBytes, err := json.Marshal(responseMsg)
	assert.NoError(t, err)

	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return path == syftURL.ToLocalPath()+"/"+requestID+".request"
	})).Return(io.NopCloser(bytes.NewReader(requestBytes)), nil)
	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return path == syftURL.ToLocalPath()+"/"+requestID+".response"
	})).Return(nil, ErrMsgNotFound)

	// Call PollForResponse
	result, err := service.PollForResponse(context.Background(), req)
	assert.Error(t, err)
	assert.Equal(t, ErrPollTimeout, err)
	assert.Nil(t, result)

	// Verify mock expectations
	store.AssertExpectations(t)
}
