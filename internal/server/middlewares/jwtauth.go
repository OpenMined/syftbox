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
	bearerPrefix = "Bearer "
	authHeader   = "Authorization"
)

// JWTAuth creates a Gin middleware function that validates access tokens.
// It requires the AuthService to access token validation logic and configuration.
func JWTAuth(authService *auth.AuthService) gin.HandlerFunc {
	if !authService.IsEnabled() {
		slog.Info("auth middleware disabled")

		return func(ctx *gin.Context) {
			user := ctx.Query("user")
			if user == "" {
				ctx.PureJSON(http.StatusForbidden, gin.H{
					"error": "'user' query param required",
				})
				ctx.Abort()
				return
			}
			ctx.Set("user", user)
			ctx.Next()
		}
	}

	slog.Info("auth middleware enabled")

	return func(ctx *gin.Context) {
		authHeaderValue := ctx.GetHeader(authHeader)
		if authHeaderValue == "" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authorization header required",
			})
			return
		}

		// Check if the header starts with "Bearer "
		if !strings.HasPrefix(authHeaderValue, bearerPrefix) {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "bearer token required",
			})
			return
		}

		// Extract the token string
		tokenString := strings.TrimPrefix(authHeaderValue, bearerPrefix)
		if tokenString == "" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "token missing",
			})
			return
		}

		// Validate the token using the method added to AuthService
		claims, err := authService.ValidateAccessToken(ctx, tokenString)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		ctx.Set("user", claims.Subject)
		ctx.Next()
	}
}
