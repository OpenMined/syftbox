package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// TimeoutConfig contains configuration for the timeout middleware
type TimeoutConfig struct {
	// Timeout is the maximum duration a request is allowed to take
	Timeout time.Duration
	// ExcludedPaths are paths that should not be subject to timeout
	ExcludedPaths map[string]bool
}

// Timeout creates a middleware that aborts requests if they exceed the timeout
func Timeout(config TimeoutConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if path is excluded from timeout
		if config.ExcludedPaths != nil && config.ExcludedPaths[c.FullPath()] {
			c.Next()
			return
		}

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
		defer cancel()

		// Update the request with the new context
		c.Request = c.Request.WithContext(ctx)

		// Use a channel to signal request completion
		done := make(chan struct{})
		
		// Execute the request in a goroutine
		go func() {
			c.Next()
			close(done)
		}()

		// Wait for request completion or timeout
		select {
		case <-done:
			// Request completed normally
			return
		case <-ctx.Done():
			// Request timed out
			if ctx.Err() == context.DeadlineExceeded {
				slog.Warn("Request timed out", "path", c.FullPath(), "method", c.Request.Method)
				c.AbortWithStatusJSON(http.StatusGatewayTimeout, gin.H{
					"error": "Request timed out",
				})
			}
		}
	}
}
