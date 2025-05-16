package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// TokenAuthConfig contains the configuration for token-based authentication.
type TokenAuthConfig struct {
	// Token is the authentication token.
	Token string
}

// TokenAuth creates a middleware for token authentication.
func TokenAuth(config TokenAuthConfig) gin.HandlerFunc {
	if config.Token == "" {
		slog.Info("auth disabled")
		return func(c *gin.Context) {
			c.Next()
		}
	} else {
		slog.Info("auth enabled")
	}

	return func(c *gin.Context) {
		// Get token from query parameter or header
		token := c.GetHeader("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")

		// If no token in header, try query parameter
		if token == "" {
			token = c.Query("token")
		}

		// Validate token
		if token != config.Token {
			slog.Debug("Invalid authentication token", "ip", c.ClientIP(), "path", c.FullPath())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthorized",
			})
			return
		}

		// Add token info to context for potential use downstream
		c.Set("authenticated", true)

		// Continue to the next handler
		c.Next()
	}
}
