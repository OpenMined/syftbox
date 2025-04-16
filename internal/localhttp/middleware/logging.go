package middleware

import (
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequestLogger logs request details.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		// Log after request
		status := c.Writer.Status()
		method := c.Request.Method
		path := c.Request.URL.Path
		latency := c.Writer.Header().Get("X-Response-Time")
		clientIP := c.ClientIP()

		// Only log non-health check requests to reduce noise
		if !strings.Contains(path, "/health") {
			level := slog.LevelInfo
			// Log errors with higher level
			if status >= 400 {
				level = slog.LevelWarn
			}
			if status >= 500 {
				level = slog.LevelError
			}

			slog.Log(c.Request.Context(), level, "HTTP Request",
				"status", status,
				"method", method,
				"path", path,
				"latency", latency,
				"ip", clientIP,
			)
		}
	}
}
