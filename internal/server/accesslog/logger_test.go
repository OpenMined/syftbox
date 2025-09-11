package accesslog

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessLogger(t *testing.T) {
	// Create temp directory for logs
	tempDir, err := os.MkdirTemp("", "accesslog_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create logger
	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	// Setup gin context
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	
	// Setup request
	ctx.Request = httptest.NewRequest("GET", "/test/path", nil)
	ctx.Request.Header.Set("User-Agent", "TestAgent/1.0")
	ctx.Set("user", "test@example.com")

	// Test logging access
	t.Run("LogWrite", func(t *testing.T) {
		logger.LogAccess(ctx, "/test/file.txt", AccessTypeWrite, acl.AccessWrite, true, "")
		
		// Give time for async write
		time.Sleep(100 * time.Millisecond)
		
		// Check log file exists
		userDir := filepath.Join(tempDir, sanitizeUsername("test@example.com"))
		files, err := os.ReadDir(userDir)
		require.NoError(t, err)
		assert.Greater(t, len(files), 0)
	})

	t.Run("LogRead", func(t *testing.T) {
		logger.LogAccess(ctx, "/test/file2.txt", AccessTypeRead, acl.AccessRead, false, "permission denied")
		
		// Give time for async write
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("GetUserLogs", func(t *testing.T) {
		// Wait for logs to be written
		time.Sleep(200 * time.Millisecond)
		
		logs, err := logger.GetUserLogs("test@example.com", 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(logs), 2)
		
		// Check log entries
		foundWrite := false
		foundRead := false
		for _, log := range logs {
			if log.Path == "/test/file.txt" && log.AccessType == AccessTypeWrite {
				foundWrite = true
				assert.True(t, log.Allowed)
				assert.Equal(t, "test@example.com", log.User)
				assert.Equal(t, "TestAgent/1.0", log.UserAgent)
			}
			if log.Path == "/test/file2.txt" && log.AccessType == AccessTypeRead {
				foundRead = true
				assert.False(t, log.Allowed)
				assert.Equal(t, "permission denied", log.DeniedReason)
			}
		}
		assert.True(t, foundWrite, "Write log entry not found")
		assert.True(t, foundRead, "Read log entry not found")
	})
}

func TestLogCreation(t *testing.T) {
	// Create temp directory for logs
	tempDir, err := os.MkdirTemp("", "accesslog_creation_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create logger
	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	// Setup gin context
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("GET", "/test", nil)
	ctx.Set("user", "create@test.com")

	// Generate log entries
	for i := 0; i < 5; i++ {
		logger.LogAccess(ctx, "/test/file.txt", AccessTypeWrite, acl.AccessWrite, true, "")
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for logs to be written
	time.Sleep(100 * time.Millisecond)

	// Check that log files exist
	userDir := filepath.Join(tempDir, sanitizeUsername("create@test.com"))
	files, err := os.ReadDir(userDir)
	require.NoError(t, err)
	
	// Should have at least one log file
	assert.GreaterOrEqual(t, len(files), 1, "Should have created log files")
}

func TestSanitizeUsername(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user@example.com", "user@example.com"},
		{"user with spaces", "user_with_spaces"},
		{"user/with/slashes", "user_with_slashes"},
		{"user:with:colons", "user_with_colons"},
		{"user.name-123_test@domain.org", "user.name-123_test@domain.org"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeUsername(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAccessLogMiddleware(t *testing.T) {
	// Create temp directory for logs
	tempDir, err := os.MkdirTemp("", "middleware_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create logger and middleware
	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	middleware := NewMiddleware(logger)

	// Setup gin router with middleware
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Handler())
	
	// Add test route
	router.GET("/test", func(ctx *gin.Context) {
		// Get logger from context
		al := GetAccessLogger(ctx)
		assert.NotNil(t, al)
		
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Make request
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
}