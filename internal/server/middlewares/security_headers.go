package middlewares

import (
	"github.com/gin-gonic/gin"
)

// SubdomainSecurityHeaders adds security headers specifically for subdomain requests
func SubdomainSecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only add headers if this is a subdomain request
		if IsSubdomainRequest(c) {
			// Prevent clickjacking attacks
			c.Header("X-Frame-Options", "SAMEORIGIN") // Allow framing from same origin

			// Prevent MIME type sniffing
			c.Header("X-Content-Type-Options", "nosniff")

			// Enable XSS protection
			c.Header("X-XSS-Protection", "1; mode=block")

			// Control referrer information
			c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		}

		// Continue to the next handler
		c.Next()
	}
}
