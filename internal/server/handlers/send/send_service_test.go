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
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
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

// MockBlobService is a mock implementation of blob.Service
type MockBlobService struct {
	mock.Mock
}

func (m *MockBlobService) Backend() blob.IBlobBackend {
	args := m.Called()
	return args.Get(0).(blob.IBlobBackend)
}

func (m *MockBlobService) Index() blob.IBlobIndex {
	args := m.Called()
	return args.Get(0).(blob.IBlobIndex)
}

func (m *MockBlobService) OnBlobChange(callback blob.BlobChangeCallback) {
	m.Called(callback)
}

// MockBlobIndex is a mock implementation of blob.IBlobIndex
type MockBlobIndex struct {
	mock.Mock
}

func (m *MockBlobIndex) Get(key string) (*blob.BlobInfo, bool) {
	args := m.Called(key)
	return args.Get(0).(*blob.BlobInfo), args.Bool(1)
}

func (m *MockBlobIndex) Set(blob *blob.BlobInfo) error {
	args := m.Called(blob)
	return args.Error(0)
}

func (m *MockBlobIndex) SetMany(blobs []*blob.BlobInfo) error {
	args := m.Called(blobs)
	return args.Error(0)
}

func (m *MockBlobIndex) Remove(key string) error {
	args := m.Called(key)
	return args.Error(0)
}

func (m *MockBlobIndex) List() ([]*blob.BlobInfo, error) {
	args := m.Called()
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) Iter() func(func(*blob.BlobInfo) bool) {
	args := m.Called()
	return args.Get(0).(func(func(*blob.BlobInfo) bool))
}

func (m *MockBlobIndex) Count() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockBlobIndex) FilterByKeyGlob(pattern string) ([]*blob.BlobInfo, error) {
	args := m.Called(pattern)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterByPrefix(prefix string) ([]*blob.BlobInfo, error) {
	args := m.Called(prefix)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterBySuffix(suffix string) ([]*blob.BlobInfo, error) {
	args := m.Called(suffix)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterByTime(filter blob.TimeFilter) ([]*blob.BlobInfo, error) {
	args := m.Called(filter)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterAfterTime(after time.Time) ([]*blob.BlobInfo, error) {
	args := m.Called(after)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterBeforeTime(before time.Time) ([]*blob.BlobInfo, error) {
	args := m.Called(before)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

// MockBlobBackend is a mock implementation of blob.IBlobBackend
type MockBlobBackend struct {
	mock.Mock
}

func (m *MockBlobBackend) GetObject(ctx context.Context, key string) (*blob.GetObjectResponse, error) {
	args := m.Called(ctx, key)
	return args.Get(0).(*blob.GetObjectResponse), args.Error(1)
}

func (m *MockBlobBackend) GetObjectPresigned(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockBlobBackend) PutObject(ctx context.Context, params *blob.PutObjectParams) (*blob.PutObjectResponse, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*blob.PutObjectResponse), args.Error(1)
}

func (m *MockBlobBackend) PutObjectPresigned(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockBlobBackend) DeleteObject(ctx context.Context, key string) (bool, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Error(1)
}

func (m *MockBlobBackend) ListObjects(ctx context.Context) ([]*blob.BlobInfo, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobBackend) Delegate() any {
	args := m.Called()
	return args.Get(0)
}

// Helper function to create a mock ACL service for testing
func createMockACLService() *acl.ACLService {
	// Create mock dependencies
	mockIndex := &MockBlobIndex{}
	mockBackend := &MockBlobBackend{}
	mockBlobService := &MockBlobService{}

	// Set up common expectations
	mockBlobService.On("Index").Return(mockIndex)
	mockBlobService.On("Backend").Return(mockBackend)
	mockBlobService.On("OnBlobChange", mock.AnythingOfType("blob.BlobChangeCallback")).Return()

	// Set up index expectations with sensible defaults
	mockIndex.On("FilterBySuffix", mock.AnythingOfType("string")).Return([]*blob.BlobInfo{}, nil)
	mockIndex.On("Iter").Return(func(yield func(*blob.BlobInfo) bool) {
		// Empty iterator by default
	})
	mockIndex.On("Count").Return(0)

	return acl.NewACLService(mockBlobService)
}

// Helper function to create a SendService for testing
func createTestSendService(dispatcher MessageDispatcher, store RPCMsgStore, cfg *Config) *SendService {
	if cfg == nil {
		cfg = &Config{
			DefaultTimeout:      1 * time.Second,
			MaxTimeout:          10 * time.Second,
			ObjectPollInterval:  200 * time.Millisecond,
			RequestCheckTimeout: 200 * time.Millisecond,
			MaxBodySize:         4 << 20, // 4MB
		}
	}

	// Create a mock ACL service that allows all access
	aclService := createMockACLService()

	return &SendService{
		dispatcher: dispatcher,
		store:      store,
		cfg:        cfg,
		acl:        aclService,
	}
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

	service := createTestSendService(dispatcher, store, cfg)
	assert.NotNil(t, service)
	assert.Equal(t, cfg, service.cfg)

	// Test with nil config (should use defaults)
	service = createTestSendService(dispatcher, store, nil)
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
	service := createTestSendService(dispatcher, store, nil)

	// Create test data - use consistent user and datasite names
	from := "test@datasite.com" // User should be the owner of the datasite
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
	service := createTestSendService(dispatcher, store, nil)

	// Create test data - use consistent user and datasite names
	from := "test@datasite.com" // User should be the owner of the datasite
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
	service := createTestSendService(dispatcher, store, nil)

	// Create test data - use consistent user and datasite names
	requestID := uuid.New().String()
	from := "test@datasite.com" // User should be the owner of the datasite
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

	// Mock request path (any path ending with .request)
	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".request")
	})).Return(io.NopCloser(bytes.NewReader(requestBytes)), nil)

	// Mock response path (any path ending with .response)
	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".response")
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
	service := createTestSendService(dispatcher, store, nil)

	// Create test data - use consistent user and datasite names
	requestID := uuid.New().String()
	from := "test@datasite.com" // User should be the owner of the datasite
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
	service := createTestSendService(dispatcher, store, cfg)

	// Create test data - use consistent user and datasite names
	requestID := uuid.New().String()
	from := "test@datasite.com" // User should be the owner of the datasite
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

	// Mock request path (any path ending with .request)
	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".request")
	})).Return(io.NopCloser(bytes.NewReader(requestBytes)), nil)

	// Mock response path to return not found (timeout scenario)
	store.On("GetMsg", mock.Anything, mock.MatchedBy(func(path string) bool {
		return strings.HasSuffix(path, ".response")
	})).Return(nil, ErrMsgNotFound)

	// Call PollForResponse
	result, err := service.PollForResponse(context.Background(), req)
	assert.Error(t, err)
	assert.Equal(t, ErrPollTimeout, err)
	assert.Nil(t, result)

	// Verify mock expectations
	store.AssertExpectations(t)
}
