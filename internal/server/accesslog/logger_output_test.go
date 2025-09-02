package accesslog

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/stretchr/testify/require"
)

// TestLogOutputExample demonstrates what the actual log output looks like
// This test is useful for CI to validate the log format
func TestLogOutputExample(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping output example in short mode")
	}

	tempDir, err := os.MkdirTemp("", "accesslog_output")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logger, err := New(tempDir, slog.Default())
	require.NoError(t, err)
	defer logger.Close()

	// Setup test context
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("GET", "/example/data.csv", nil)
	ctx.Request.Header.Set("User-Agent", syftsdk.SyftBoxUserAgent)
	ctx.Request.RemoteAddr = "192.168.1.100:54321"
	ctx.Set("user", "demo@syftbox.org")

	// Log different types of access
	examples := []struct {
		method     string
		path       string
		accessType AccessType
		allowed    bool
		reason     string
	}{
		{"GET", "/demo@syftbox.org/public/dataset.csv", AccessTypeRead, true, ""},
		{"PUT", "/demo@syftbox.org/private/model.pkl", AccessTypeWrite, false, "ACL denied: private directory"},
		{"DELETE", "/admin/config.yaml", AccessTypeAdmin, false, "Admin access required"},
	}

	fmt.Println("\n=== Example Access Log Entries ===")
	fmt.Printf("User-Agent: %s\n", syftsdk.SyftBoxUserAgent)
	fmt.Println()

	for i, ex := range examples {
		ctx.Request.Method = ex.method
		logger.LogAccess(ctx, ex.path, ex.accessType, acl.AccessRead, ex.allowed, ex.reason)
		
		// Wait briefly
		time.Sleep(50 * time.Millisecond)
		
		// Read back and display
		logs, err := logger.GetUserLogs("demo@syftbox.org", i+1)
		require.NoError(t, err)
		require.Equal(t, i+1, len(logs))
		
		// Pretty print the latest log entry
		entry := logs[len(logs)-1]
		jsonBytes, err := json.MarshalIndent(entry, "", "  ")
		require.NoError(t, err)
		
		fmt.Printf("Entry %d: %s %s\n", i+1, ex.method, ex.path)
		fmt.Println(string(jsonBytes))
		fmt.Println()
	}
	
	fmt.Println("=== Log entries successfully written and validated ===")
}