package middlewares

import (
	"net/http"

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

var (
	defaultCORS = cors.New(defaultCORSConfig)
)

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsSubdomainRequest(c) {
			origin := c.Request.Header.Get("Origin")

			if len(origin) != 0 {
				// For subdomain requests, apply strict CORS policy
				// Only allow same-origin requests with credentials
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, X-CSRF-Token")
				c.Header("Access-Control-Max-Age", "86400")
			}

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
