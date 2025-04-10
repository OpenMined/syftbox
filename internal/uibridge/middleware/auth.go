package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// TokenAuthConfig contains the configuration for token-based authentication.
type TokenAuthConfig struct {
	// Token is the authentication token.
	Token string
}

// TokenAuth creates a middleware for token authentication.
func TokenAuth(config TokenAuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from query parameter or header
		token := c.Query("token")
		if token == "" {
			token = c.GetHeader("Authorization")
			// Remove "Bearer " prefix if present (case-insensitive)
			tokenLower := strings.ToLower(token)
			if strings.HasPrefix(tokenLower, "bearer ") {
				token = token[7:]
			}
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
