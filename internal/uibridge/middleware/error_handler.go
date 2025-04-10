package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	appErrors "github.com/yashgorana/syftbox-go/internal/uibridge/errors"
)

// ErrorResponse represents an error response
type ErrorResponse = appErrors.ErrorResponse

// ErrorHandler creates a middleware for handling errors
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		// Only handle errors if there are any
		if len(c.Errors) == 0 {
			return
		}

		// Get the last error
		err := c.Errors.Last().Err

		// Check if the error is an AppError
		var appErr *appErrors.AppError
		if errors.As(err, &appErr) {
			// Log the error with stack trace for server errors
			if appErr.Status >= 500 && appErr.Internal != nil {
				slog.Error("Server error",
					"error", appErr.Error(),
					"code", appErr.Code,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
					"status", appErr.Status,
					"stack", string(debug.Stack()),
				)
			} else if appErr.Status >= 400 {
				slog.Warn("Client error",
					"error", appErr.Error(),
					"code", appErr.Code,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
					"status", appErr.Status,
				)
			}

			// Create an error response
			response := ErrorResponse{
				Code:    appErr.Code,
				Message: appErr.Message,
				Details: appErr.Details,
			}

			// Send the error response
			c.JSON(appErr.Status, response)
			return
		}

		// For non-AppError errors, return a generic error
		slog.Error("Unhandled error",
			"error", err.Error(),
			"path", c.Request.URL.Path,
			"method", c.Request.Method,
			"stack", string(debug.Stack()),
		)

		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Code:    "internal_error",
			Message: "An unexpected error occurred",
		})
	}
}
