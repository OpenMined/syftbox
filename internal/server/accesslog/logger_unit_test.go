package accesslog

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimestampFormatting(t *testing.T) {
	// Test that timestamps are formatted in human-readable format
	entry := AccessLogEntry{
		Timestamp:  time.Date(2024, 12, 2, 14, 30, 22, 123456789, time.UTC),
		Path:       "/test/file.txt",
		AccessType: AccessTypeRead,
		User:       "test@example.com",
		IP:         "192.168.1.1",
		UserAgent:  "TestAgent/1.0",
		Method:     "GET",
		StatusCode: 200,
		Allowed:    true,
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	require.NoError(t, err)

	// Check the timestamp format
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	timestamp := result["timestamp"].(string)
	assert.Equal(t, "2024-12-02 14:30:22.123 UTC", timestamp, "Timestamp should be in human-readable format")
	
	// Verify we can unmarshal it back
	var decoded AccessLogEntry
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, entry.Timestamp.Unix(), decoded.Timestamp.Unix())
}

func TestLogDirectoryStructure(t *testing.T) {
	// Test that logs are organized by user in separate directories
	tempDir, err := os.MkdirTemp("", "accesslog_structure_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	users := []string{
		"alice@example.com",
		"bob@test.org",
		"charlie+test@gmail.com",
	}

	// Setup gin context
	gin.SetMode(gin.TestMode)
	
	for _, user := range users {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest("GET", "/test", nil)
		ctx.Set("user", user)
		
		logger.LogAccess(ctx, "/test/file.txt", AccessTypeRead, acl.AccessRead, true, "")
	}

	// Wait for logs to be written
	time.Sleep(200 * time.Millisecond)

	// Verify directory structure
	for _, user := range users {
		sanitized := sanitizeUsername(user)
		userDir := filepath.Join(tempDir, sanitized)
		
		info, err := os.Stat(userDir)
		require.NoError(t, err, "User directory should exist for %s", user)
		assert.True(t, info.IsDir(), "Should be a directory")
		
		// Check permissions (0700)
		if runtime.GOOS != "windows" {
			mode := info.Mode().Perm()
			assert.Equal(t, os.FileMode(0700), mode&0700, "Directory should have 0700 permissions")
		}
		
		// Check log files exist
		files, err := os.ReadDir(userDir)
		require.NoError(t, err)
		assert.Greater(t, len(files), 0, "Should have at least one log file for %s", user)
	}
}

func TestUserAgentFormat(t *testing.T) {
	// Test that user agent contains expected information
	tempDir, err := os.MkdirTemp("", "accesslog_useragent_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	// Create test with specific user agent
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	
	testUA := fmt.Sprintf("SyftBox/1.0.0 (abc123; %s/%s; Go/%s; TestOS/1.0)", 
		runtime.GOOS, runtime.GOARCH, runtime.Version())
	
	ctx.Request = httptest.NewRequest("GET", "/test", nil)
	ctx.Request.Header.Set("User-Agent", testUA)
	ctx.Set("user", "test@example.com")
	
	logger.LogAccess(ctx, "/test/file.txt", AccessTypeRead, acl.AccessRead, true, "")
	
	// Wait and read logs
	time.Sleep(100 * time.Millisecond)
	
	logs, err := logger.GetUserLogs("test@example.com", 10)
	require.NoError(t, err)
	require.Greater(t, len(logs), 0)
	
	// Verify user agent is captured correctly
	assert.Equal(t, testUA, logs[0].UserAgent)
	assert.Contains(t, logs[0].UserAgent, runtime.GOOS)
	assert.Contains(t, logs[0].UserAgent, runtime.GOARCH)
	assert.Contains(t, logs[0].UserAgent, "Go/")
}

func TestAccessTypeLogging(t *testing.T) {
	// Test different access types are logged correctly
	tempDir, err := os.MkdirTemp("", "accesslog_types_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("GET", "/test", nil)
	ctx.Set("user", "test@example.com")

	testCases := []struct {
		accessType  AccessType
		accessLevel acl.AccessLevel
		allowed     bool
		reason      string
	}{
		{AccessTypeRead, acl.AccessRead, true, ""},
		{AccessTypeWrite, acl.AccessWrite, true, ""},
		{AccessTypeAdmin, acl.AccessAdmin, false, "insufficient permissions"},
		{AccessTypeDeny, acl.AccessRead, false, "blacklisted path"},
	}

	for _, tc := range testCases {
		logger.LogAccess(ctx, "/test/file.txt", tc.accessType, tc.accessLevel, tc.allowed, tc.reason)
	}

	// Wait and verify
	time.Sleep(200 * time.Millisecond)
	
	logs, err := logger.GetUserLogs("test@example.com", 10)
	require.NoError(t, err)
	assert.Equal(t, len(testCases), len(logs))

	for i, tc := range testCases {
		assert.Equal(t, tc.accessType, logs[i].AccessType)
		assert.Equal(t, tc.allowed, logs[i].Allowed)
		assert.Equal(t, tc.reason, logs[i].DeniedReason)
	}
}

func TestLogRotationFileNaming(t *testing.T) {
	// Test that log files are named correctly with dates
	tempDir, err := os.MkdirTemp("", "accesslog_naming_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("GET", "/test", nil)
	ctx.Set("user", "test@example.com")

	logger.LogAccess(ctx, "/test/file.txt", AccessTypeRead, acl.AccessRead, true, "")
	
	time.Sleep(100 * time.Millisecond)

	// Check file naming
	userDir := filepath.Join(tempDir, "test@example.com")
	files, err := os.ReadDir(userDir)
	require.NoError(t, err)
	require.Equal(t, 1, len(files))

	// File should be named access_YYYYMMDD.log
	expectedPrefix := fmt.Sprintf("access_%s", time.Now().Format("20060102"))
	assert.True(t, strings.HasPrefix(files[0].Name(), expectedPrefix))
	assert.True(t, strings.HasSuffix(files[0].Name(), ".log"))
}

func TestIPAddressCapture(t *testing.T) {
	// Test that IP addresses are captured correctly
	tempDir, err := os.MkdirTemp("", "accesslog_ip_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("GET", "/test", nil)
	ctx.Request.RemoteAddr = "10.0.0.1:12345"
	ctx.Set("user", "test@example.com")

	logger.LogAccess(ctx, "/test/file.txt", AccessTypeRead, acl.AccessRead, true, "")
	
	time.Sleep(100 * time.Millisecond)

	logs, err := logger.GetUserLogs("test@example.com", 1)
	require.NoError(t, err)
	require.Equal(t, 1, len(logs))

	// Gin's ClientIP should extract the IP correctly
	assert.NotEmpty(t, logs[0].IP)
	// The actual IP might be transformed by gin's ClientIP logic
	// but it should at least not be empty
}

func TestConcurrentLogging(t *testing.T) {
	// Test that concurrent logging works correctly
	tempDir, err := os.MkdirTemp("", "accesslog_concurrent_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	gin.SetMode(gin.TestMode)
	
	// Log from multiple goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest("GET", fmt.Sprintf("/test%d", id), nil)
			ctx.Set("user", "concurrent@example.com")
			
			logger.LogAccess(ctx, fmt.Sprintf("/test/file%d.txt", id), AccessTypeRead, acl.AccessRead, true, "")
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	time.Sleep(200 * time.Millisecond)

	// Verify all logs were written
	logs, err := logger.GetUserLogs("concurrent@example.com", 20)
	require.NoError(t, err)
	assert.Equal(t, 10, len(logs))
}