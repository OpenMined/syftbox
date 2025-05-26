package middlewares

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/auth" // Import your auth package
	"github.com/openmined/syftbox/internal/utils"
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
			// expect user to be an email address
			user := ctx.Query("user")
			if !utils.IsValidEmail(user) {
				ctx.Error(fmt.Errorf("invalid email"))
				ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": "invalid email",
				})
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
			ctx.Error(fmt.Errorf("authorization header required"))
			ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "authorization header required",
			})
			return
		}

		// Check if the header starts with "Bearer "
		if !strings.HasPrefix(authHeaderValue, bearerPrefix) {
			ctx.Error(fmt.Errorf("bearer token required"))
			ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "bearer token required",
			})
			return
		}

		// Extract the token string
		tokenString := strings.TrimPrefix(authHeaderValue, bearerPrefix)
		if tokenString == "" {
			ctx.Error(fmt.Errorf("token missing"))
			ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "token missing",
			})
			return
		}

		// Validate the token using the method added to AuthService
		claims, err := authService.ValidateAccessToken(ctx, tokenString)
		if err != nil {
			ctx.Error(err)
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		ctx.Set("user", claims.Subject)
		ctx.Next()
	}
}
