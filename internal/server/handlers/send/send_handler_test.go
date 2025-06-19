package send

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

//go:embed *.html
var templateFS embed.FS

// MockSendService implements the SendService interface for testing
type MockSendService struct {
	mock.Mock
}

func (m *MockSendService) SendMessage(ctx context.Context, req *MessageRequest, bodyBytes []byte) (*SendResult, error) {
	args := m.Called(ctx, req, bodyBytes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SendResult), args.Error(1)
}

func (m *MockSendService) PollForResponse(ctx context.Context, req *PollObjectRequest) (*PollResult, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*PollResult), args.Error(1)
}

func (m *MockSendService) constructPollURL(requestID string, syftURL utils.SyftBoxURL, from string, asRaw bool) string {
	args := m.Called(requestID, syftURL, from, asRaw)
	return args.String(0)
}

func (m *MockSendService) GetConfig() *Config {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*Config)
}

// Helper function to create a test gin context
func createTestContext(
	method string,
	url string,
	body io.Reader,
	query_params map[string]string,
	headers map[string]string,
) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Build URL with query parameters
	if len(query_params) > 0 {
		params := make([]string, 0, len(query_params))
		for k, v := range query_params {
			params = append(params, k+"="+v)
		}
		url = url + "?" + strings.Join(params, "&")
	}

	req := httptest.NewRequest(method, url, body)

	// Add headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	c.Request = req
	return c, w
}

func TestSendHandler_SendMsg_SuccessWithResponse(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	body := `{"key": "value"}`
	query_params := map[string]string{
		"x-syft-url":  "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from": "testuser@example.com",
		"timeout":     "1000",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	c, w := createTestContext("POST", "/send/msg/", strings.NewReader(body), query_params, headers)

	// Mock expectations
	expectedResult := &SendResult{
		Status:    http.StatusOK,
		RequestID: "test-request-id",
		Response: map[string]interface{}{
			"message": "success",
		},
	}

	mockService.On("SendMessage", mock.Anything, mock.MatchedBy(func(req *MessageRequest) bool {
		return req.From == "testuser@example.com" && req.Method == "POST"
	}), []byte(body)).Return(expectedResult, nil)
	mockService.On("GetConfig").Return(&Config{MaxBodySize: 4 * 1024 * 1024}) // 4MB

	// Execute
	handler.SendMsg(c)

	// Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	var response APIResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-request-id", response.RequestID)
	assert.NotNil(t, response.Data)

	mockService.AssertExpectations(t)
}

func TestSendHandler_SendMsg_SuccessWithPolling(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	body := `{"key": "value"}`
	query_params := map[string]string{
		"x-syft-url":  "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from": "testuser@example.com",
		"timeout":     "1000",
	}
	headers := map[string]string{
		"x-syft-url":   "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from":  "testuser@example.com",
		"Content-Type": "application/json",
	}

	c, w := createTestContext("POST", "/send/msg/", strings.NewReader(body), query_params, headers)

	// Mock expectations
	expectedResult := &SendResult{
		Status:    http.StatusAccepted,
		RequestID: "test-request-id",
		PollURL:   "/api/v1/send/poll?x-syft-request-id=test-request-id&x-syft-url=syft://test@datasite.com/app_data/testapp/rpc/endpoint&x-syft-from=testuser@example.com&x-syft-raw=false",
	}

	mockService.On("SendMessage", mock.Anything, mock.Anything, []byte(body)).Return(expectedResult, nil)
	mockService.On("GetConfig").Return(&Config{MaxBodySize: 4 * 1024 * 1024}) // 4MB

	// Execute
	handler.SendMsg(c)

	// Assertions
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, expectedResult.PollURL, w.Header().Get("Location"))

	var response APIResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-request-id", response.RequestID)
	assert.Equal(t, "Request has been accepted. Please check back later.", response.Message)

	// Check PollInfo in response
	pollInfo, ok := response.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, expectedResult.PollURL, pollInfo["poll_url"])

	mockService.AssertExpectations(t)
}

func TestSendHandler_SendMsg_InvalidRequestBinding(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data - missing required fields
	body := `{"key": "value"}`
	query_params := map[string]string{
		"x-syft-url": "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	c, w := createTestContext("POST", "/send/msg/", strings.NewReader(body), query_params, headers)

	// Execute
	handler.SendMsg(c)

	// Assertions
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response APIError
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, ErrorInvalidRequest, response.Error)
	assert.Contains(t, response.Message, "required")
}

func TestSendHandler_SendMsg_BodyTooLarge(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	largeBody := strings.Repeat("a", 5*1024*1024) // 5MB
	query_params := map[string]string{
		"x-syft-url":  "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from": "testuser@example.com",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	c, w := createTestContext("POST", "/send/msg/", strings.NewReader(largeBody), query_params, headers)

	// Mock expectations
	mockService.On("GetConfig").Return(&Config{MaxBodySize: 4 * 1024 * 1024}) // 4MB

	// Execute
	handler.SendMsg(c)

	// Assertions
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response APIError
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, ErrorInvalidRequest, response.Error)
	assert.Contains(t, response.Message, "too large")
}

func TestSendHandler_SendMsg_ServiceError(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	body := `{"key": "value"}`
	query_params := map[string]string{
		"x-syft-url":  "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from": "testuser@example.com",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	c, w := createTestContext("POST", "/send/msg/", strings.NewReader(body), query_params, headers)

	// Mock expectations
	mockService.On("SendMessage", mock.Anything, mock.Anything, []byte(body)).Return(nil, errors.New("service error"))
	mockService.On("GetConfig").Return(&Config{MaxBodySize: 4 * 1024 * 1024}) // 4MB
	// Execute
	handler.SendMsg(c)

	// Assertions
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response APIError
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, ErrorInternal, response.Error)
	assert.Equal(t, "service error", response.Message)

	mockService.AssertExpectations(t)
}

func TestSendHandler_PollForResponse_Success(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	query_params := map[string]string{
		"x-syft-request-id": "test-request-id",
		"x-syft-url":        "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from":       "test-user",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	c, w := createTestContext("GET", "/send/poll/", nil, query_params, headers)

	// Mock expectations
	expectedResult := &PollResult{
		Status:    http.StatusOK,
		RequestID: "test-request-id",
		Response: map[string]interface{}{
			"message": "success",
		},
	}

	mockService.On("PollForResponse", mock.Anything, mock.MatchedBy(func(req *PollObjectRequest) bool {
		return req.RequestID == "test-request-id" && req.From == "test-user"
	})).Return(expectedResult, nil)

	// Execute
	handler.PollForResponse(c)

	// Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	var response APIResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-request-id", response.RequestID)
	assert.NotNil(t, response.Data)

	mockService.AssertExpectations(t)
}

func TestSendHandler_PollForResponse_TimeoutWithJSON(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	query_params := map[string]string{
		"x-syft-request-id": "test-request-id",
		"x-syft-url":        "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from":       "test-user",
		"timeout":           "1000",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	c, w := createTestContext("GET", "/send/poll/", nil, query_params, headers)

	// Mock expectations
	pollURL := "/api/v1/send/poll?x-syft-request-id=test-request-id&x-syft-url=syft://test@datasite.com/app_data/testapp/rpc/endpoint&x-syft-from=test-user&x-syft-raw=false"

	mockService.On("PollForResponse", mock.Anything, mock.Anything).Return(nil, ErrPollTimeout)
	mockService.On("constructPollURL", "test-request-id", mock.Anything, "test-user", false).Return(pollURL)

	// Execute
	handler.PollForResponse(c)

	// Assertions
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, pollURL, w.Header().Get("Location"))
	assert.Equal(t, "1", w.Header().Get("Retry-After"))

	var response APIError
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, ErrorTimeout, response.Error)
	assert.Equal(t, "test-request-id", response.RequestID)

	mockService.AssertExpectations(t)
}

func TestSendHandler_PollForResponse_NotFound(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	query_params := map[string]string{
		"x-syft-request-id": "test-request-id",
		"x-syft-url":        "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from":       "test-user",
	}
	headers := map[string]string{
		"x-syft-request-id": "test-request-id",
		"x-syft-url":        "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from":       "test-user",
	}

	c, w := createTestContext("GET", "/send/poll/", nil, query_params, headers)

	// Mock expectations
	mockService.On("PollForResponse", mock.Anything, mock.Anything).Return(nil, ErrNoRequest)

	// Execute
	handler.PollForResponse(c)

	// Assertions
	assert.Equal(t, http.StatusNotFound, w.Code)

	var response APIError
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, ErrorNotFound, response.Error)
	assert.Equal(t, "test-request-id", response.RequestID)

	mockService.AssertExpectations(t)
}

func TestSendHandler_PollForResponse_InvalidRequest(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data - missing required fields
	query_params := map[string]string{
		"x-syft-from": "test-user",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
		// Missing required headers
	}

	c, w := createTestContext("GET", "/send/poll/", nil, query_params, headers)

	// Execute
	handler.PollForResponse(c)

	// Assertions
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response APIError
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, ErrorInvalidRequest, response.Error)
	assert.Contains(t, response.Message, "required")
}

func TestSendHandler_PollForResponse_ServiceError(t *testing.T) {
	// Setup
	mockService := &MockSendService{}
	handler := &SendHandler{service: mockService}

	// Test data
	query_params := map[string]string{
		"x-syft-request-id": "test-request-id",
		"x-syft-url":        "syft://test@datasite.com/app_data/testapp/rpc/endpoint",
		"x-syft-from":       "test-user",
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	c, w := createTestContext("GET", "/send/poll/", nil, query_params, headers)

	// Mock expectations
	mockService.On("PollForResponse", mock.Anything, mock.Anything).Return(nil, errors.New("service error"))

	// Execute
	handler.PollForResponse(c)

	// Assertions
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response APIError
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, ErrorInternal, response.Error)
	assert.Equal(t, "service error", response.Message)
	assert.Equal(t, "test-request-id", response.RequestID)

	mockService.AssertExpectations(t)
}

func TestSendHandler_New(t *testing.T) {
	// Setup
	mockDispatcher := &MockMessageDispatcher{}
	mockStore := &MockRPCMsgStore{}

	// Execute
	handler := New(mockDispatcher, mockStore)

	// Assertions
	assert.NotNil(t, handler)
	assert.NotNil(t, handler.service)
}

func TestReadRequestBody_Success(t *testing.T) {
	// Setup
	body := `{"key": "value"}`
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))

	// Execute
	result, err := readRequestBody(c, 1024*1024) // 1MB limit

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, []byte(body), result)
}

func TestReadRequestBody_TooLarge(t *testing.T) {
	// Setup
	largeBody := strings.Repeat("a", 1025) // 1025 bytes
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(largeBody))

	// Execute
	result, err := readRequestBody(c, 1024) // 1KB limit

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "too large")
}

func TestReadRequestBody_ReadError(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Create a request with a body that will fail to read
	req := httptest.NewRequest("POST", "/", &failingReader{})
	c.Request = req

	// Execute
	result, err := readRequestBody(c, 1024)

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to read request body")
}

// failingReader is a reader that always fails
type failingReader struct{}

func (f *failingReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}
