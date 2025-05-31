package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Errors != nil {
			slog.Warn("HTTP request",
				"method", c.Request.Method,
				"status", c.Writer.Status(),
				"path", c.Request.URL.Path,
				"query", c.Request.URL.RawQuery,
				"errors", c.Errors.String(),
			)
		}
	}
}
