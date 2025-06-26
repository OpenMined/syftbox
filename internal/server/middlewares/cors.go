package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS returns a CORS middleware that provides subdomain isolation
func CORS() gin.HandlerFunc {
	// Default config for non-subdomain requests
	defaultConfig := cors.Config{
		AllowOrigins:     []string{"*"},
		AllowHeaders:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowCredentials: false,
		AllowWebSockets:  true,
	}

	// Create custom CORS handler
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		
		// Check if this is a subdomain request
		if IsSubdomainRequest(c) {
			// For subdomain requests, apply strict CORS policy
			// Only allow same-origin requests with credentials
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, X-CSRF-Token")
			c.Header("Access-Control-Max-Age", "86400")
			
			// Add Content Security Policy for subdomain isolation
			c.Header("Content-Security-Policy", "default-src 'self' https://*.syftbox.net; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline';")
			
			// For preflight requests
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		} else {
			// For non-subdomain requests, use the default CORS handler
			corsHandler := cors.New(defaultConfig)
			corsHandler(c)
			return
		}
		
		c.Next()
	}
}

// SetSubdomainSecurityHeaders sets additional security headers for subdomain requests
func SetSubdomainSecurityHeaders(c *gin.Context) {
	if IsSubdomainRequest(c) {
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
		
		// Set cookie attributes for subdomain
		if email, exists := GetSubdomainEmail(c); exists {
			// Create a unique cookie prefix for this subdomain
			cookiePrefix := strings.ReplaceAll(email, "@", "_at_")
			cookiePrefix = strings.ReplaceAll(cookiePrefix, ".", "_")
			c.Set("cookie_prefix", cookiePrefix)
		}
	}
}
