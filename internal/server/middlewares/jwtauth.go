package middlewares

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/auth" // Import your auth package
	// Import types for error constants
)

const (
	bearerPrefix   = "Bearer "
	authHeader     = "Authorization"
	userContextKey = "user" // Key to store user identifier in Gin context
)

// JWTAuth creates a Gin middleware function that validates access tokens.
// It requires the AuthService to access token validation logic and configuration.
func JWTAuth(authService *auth.AuthService) gin.HandlerFunc {
	if !authService.IsEnabled() {
		slog.Info("auth middleware disabled")
		return func(ctx *gin.Context) {
			ctx.Next()
		}
	}
	slog.Info("auth middleware enabled")
	return func(ctx *gin.Context) {
		authHeaderValue := ctx.GetHeader(authHeader)
		if authHeaderValue == "" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header is missing",
			})
			return
		}

		// Check if the header starts with "Bearer "
		if !strings.HasPrefix(authHeaderValue, bearerPrefix) {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header format must be Bearer {token}",
			})
			return
		}

		// Extract the token string
		tokenString := strings.TrimPrefix(authHeaderValue, bearerPrefix)
		if tokenString == "" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Token is missing",
			})
			return
		}

		// Validate the token using the method added to AuthService
		claims, err := authService.ValidateAccessToken(ctx, tokenString)
		if err != nil {
			// Consider mapping specific errors (expired, invalid signature etc.)
			// For now, a general unauthorized error
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(), // Provide more detail from the validation error
			})
			return
		}

		// Token is valid, set the user identifier in the context
		ctx.Set(userContextKey, claims.Subject) // Store the Subject (user email/ID)

		// Continue to the next handler
		ctx.Next()
	}
}
