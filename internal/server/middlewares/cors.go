package middlewares

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// server cors config
var defaultCORSConfig = cors.Config{
	AllowOrigins:     []string{"*"},
	AllowHeaders:     []string{"*"},
	AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
	AllowCredentials: false,
	AllowWebSockets:  true,
}

var strictCORSConfig = cors.Config{
	AllowAllOrigins: false,
	AllowOriginFunc: func(origin string) bool {
		// todo perhaps should always have https?
		return true
	},
	AllowHeaders:     []string{"Accept", "Content-Type", "Content-Length", "Accept-Encoding", "Authorization", "X-CSRF-Token"},
	AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
	AllowCredentials: true,
	AllowWebSockets:  false,
	MaxAge:           24 * time.Hour,
}

var (
	defaultCORS = cors.New(defaultCORSConfig)
	strictCORS  = cors.New(strictCORSConfig)
)

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsSubdomainRequest(c) {
			// apply strict CORS
			strictCORS(c)

			// Prevent clickjacking
			c.Header("X-Frame-Options", "SAMEORIGIN")

			// Prevent MIME type sniffing
			c.Header("X-Content-Type-Options", "nosniff")

			// Enable XSS protection
			c.Header("X-XSS-Protection", "1; mode=block")

			// Referrer policy for privacy
			c.Header("Referrer-Policy", "same-origin")

			// Feature policy to restrict features
			c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			// Add Content Security Policy for subdomain isolation
			c.Header("Content-Security-Policy", "default-src 'self' https://*.syftbox.net; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline';")

			// For preflight requests
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		} else {
			defaultCORS(c)
		}
	}
}
