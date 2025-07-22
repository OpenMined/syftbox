package middlewares

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/auth" // Import your auth package
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/utils"
	// Import types for error constants
)

const (
	bearerPrefix = "Bearer "
	authHeader   = "Authorization"
)

// JWTAuth creates a Gin middleware function that validates access tokens.
// It requires the AuthService to access token validation logic and configuration.
func JWTAuth(authService *auth.AuthService, allowGuest bool) gin.HandlerFunc {
	if !authService.IsEnabled() {
		slog.Info("auth middleware disabled")

		return func(ctx *gin.Context) {
			// expect user to be an email address
			user := ctx.Query("user")

			// if user is not set, check if the request has a x-syft-from query parameter
			if user == "" {
				user = ctx.Query("x-syft-from")
			}

			// check if the user is a valid email address
			if !utils.IsValidEmail(user) {
				api.AbortWithError(ctx, http.StatusUnauthorized, api.CodeInvalidRequest, fmt.Errorf("invalid email"))
				return
			}
			ctx.Set("user", user)
			ctx.Next()
		}
	}

	return func(ctx *gin.Context) {
		// Check for guest access first if allowed
		if allowGuest {
			user := ctx.Query("user")
			if user == "" {
				user = ctx.Query("x-syft-from")
			}
			if user == "guest@syft.org" {
				ctx.Set("user", user)
				ctx.Next()
				return
			}
		}

		// Proceed with normal JWT authentication
		authHeaderValue := ctx.GetHeader(authHeader)
		if authHeaderValue == "" {
			api.AbortWithError(ctx, http.StatusUnauthorized, api.CodeAuthInvalidCredentials, fmt.Errorf("authorization header required"))
			return
		}

		// Check if the header starts with "Bearer "
		if !strings.HasPrefix(authHeaderValue, bearerPrefix) {
			api.AbortWithError(ctx, http.StatusUnauthorized, api.CodeAuthInvalidCredentials, fmt.Errorf("bearer token required"))
			return
		}

		// Extract the token string
		tokenString := strings.TrimPrefix(authHeaderValue, bearerPrefix)
		if tokenString == "" {
			api.AbortWithError(ctx, http.StatusUnauthorized, api.CodeAuthInvalidCredentials, fmt.Errorf("token missing"))
			return
		}

		// Validate the token using the method added to AuthService
		claims, err := authService.ValidateAccessToken(ctx, tokenString)
		if err != nil {
			api.AbortWithError(ctx, http.StatusUnauthorized, api.CodeAuthInvalidCredentials, err)
			return
		}

		ctx.Set("user", claims.Subject)
		ctx.Next()
	}
}
