package send

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestBindHeaders(t *testing.T) {
	tests := []struct {
		name           string
		inputHeaders   map[string]string
		from           string
		expectedHeaders map[string]string
	}{
		{
			name: "converts headers to lowercase",
			inputHeaders: map[string]string{
				"Content-Type":    "application/json",
				"Accept":          "*/*",
				"User-Agent":      "test-agent",
				"X-Custom-Header": "custom-value",
			},
			from: "test@example.com",
			expectedHeaders: map[string]string{
				"content-type":    "application/json",
				"accept":          "*/*",
				"user-agent":      "test-agent",
				"x-custom-header": "custom-value",
				"x-syft-from":     "test@example.com",
			},
		},
		{
			name: "removes authorization header",
			inputHeaders: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer secret-token",
				"X-Api-Key":     "api-key-value", // This should be kept
			},
			from: "test@example.com",
			expectedHeaders: map[string]string{
				"content-type": "application/json",
				"x-api-key":    "api-key-value",
				"x-syft-from":  "test@example.com",
			},
		},
		{
			name: "handles mixed case authorization header",
			inputHeaders: map[string]string{
				"Content-Type":  "application/json",
				"AUTHORIZATION": "Bearer secret-token",
				"authorization": "Bearer another-token",
				"Authorization": "Bearer yet-another-token",
			},
			from: "test@example.com",
			expectedHeaders: map[string]string{
				"content-type": "application/json",
				"x-syft-from":  "test@example.com",
			},
		},
		{
			name: "preserves all non-authorization headers",
			inputHeaders: map[string]string{
				"Content-Type":        "application/json",
				"X-Api-Key":           "api-key-123",
				"X-Custom-Auth-Token": "custom-token",
				"Cookie":              "session=abc123",
				"X-Secret-Key":        "secret-value",
			},
			from: "test@example.com",
			expectedHeaders: map[string]string{
				"content-type":        "application/json",
				"x-api-key":           "api-key-123",
				"x-custom-auth-token": "custom-token",
				"cookie":              "session=abc123",
				"x-secret-key":        "secret-value",
				"x-syft-from":         "test@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test request with headers
			req := httptest.NewRequest(http.MethodPost, "/test", nil)
			for k, v := range tt.inputHeaders {
				req.Header.Set(k, v)
			}

			// Create a gin context with the request
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = req

			// Create MessageRequest and bind headers
			msgReq := &MessageRequest{
				From: tt.from,
			}
			msgReq.BindHeaders(ctx)

			// Assert the results
			assert.Equal(t, tt.expectedHeaders, map[string]string(msgReq.Headers))
		})
	}
}