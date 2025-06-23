package middlewares

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// SubdomainEmailKey stores the extracted email in gin context
	SubdomainEmailKey = "subdomain_email"
	// SubdomainHashKey stores the subdomain hash in gin context
	SubdomainHashKey = "subdomain_hash"
	// IsSubdomainRequestKey indicates if this is a subdomain request
	IsSubdomainRequestKey = "is_subdomain_request"
)

// SubdomainConfig holds configuration for subdomain middleware
type SubdomainConfig struct {
	// MainDomain is the base domain (e.g., "syftbox.net")
	MainDomain string
	// GetVanityDomainFunc returns vanity domain config (now handles all domains)
	GetVanityDomainFunc func(domain string) (email string, path string, exists bool)
}

// SubdomainMiddleware extracts subdomain and rewrites paths for datasite access
func SubdomainMiddleware(config SubdomainConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		host := c.Request.Host
		
		// Debug logging
		c.Set("debug_original_host", host)
		
		// Remove port if present
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx]
		}
		
		// Debug logging
		c.Set("debug_host_no_port", host)
		c.Set("debug_main_domain", config.MainDomain)

		// First check if this is a vanity domain
		if config.GetVanityDomainFunc != nil {
			if email, path, exists := config.GetVanityDomainFunc(host); exists {
				c.Set(IsSubdomainRequestKey, true)
				c.Set(SubdomainEmailKey, email)
				c.Set("is_vanity_domain", true)
				c.Set("vanity_domain", host)

				// Rewrite the path
				originalPath := c.Request.URL.Path
				
				// If accessing API endpoints, don't rewrite
				if strings.HasPrefix(originalPath, "/api/") {
					c.Next()
					return
				}

				// Rewrite path to datasite format with custom path
				// Remove leading slash from original path
				if originalPath == "/" {
					originalPath = ""
				} else if strings.HasPrefix(originalPath, "/") {
					originalPath = originalPath[1:]
				}

				// Ensure path starts with /
				if !strings.HasPrefix(path, "/") {
					path = "/" + path
				}

				// Construct new path - vanity domain points to custom path
				// Handle root path case to avoid double slashes
				var newPath string
				if path == "/" {
					// Root path case: point to datasite root
					if originalPath == "" {
						newPath = "/datasites/" + email + "/"
					} else {
						newPath = "/datasites/" + email + "/" + originalPath
					}
				} else {
					// Custom path case
					if originalPath == "" {
						newPath = "/datasites/" + email + path + "/"
					} else {
						newPath = "/datasites/" + email + path + "/" + originalPath
					}
				}
				c.Request.URL.Path = newPath

				// Store original path for logging/debugging
				c.Set("original_path", originalPath)
				c.Set("rewritten_path", newPath)
				
				c.Next()
				return
			}
		}

		// Check if this is a hash-based subdomain
		if config.MainDomain != "" && isSubdomainRequest(host, config.MainDomain) {
			subdomain := extractSubdomain(host, config.MainDomain)
			
			// Debug logging
			c.Set("debug_is_subdomain", true)
			c.Set("debug_extracted_subdomain", subdomain)
			
			// Check if the subdomain is a valid hash (16 character hex)
			if len(subdomain) == 16 && isHexString(subdomain) {
				// Debug logging
				c.Set("debug_is_valid_hash", true)
				// Try to get the email for this hash
				if config.GetVanityDomainFunc != nil {
					// The GetVanityDomainFunc also handles hash lookups
					if email, path, exists := config.GetVanityDomainFunc(host); exists {
						c.Set(IsSubdomainRequestKey, true)
						c.Set(SubdomainEmailKey, email)
						c.Set(SubdomainHashKey, subdomain)
						c.Set("is_hash_subdomain", true)
						
						// Rewrite the path
						originalPath := c.Request.URL.Path
						
						// If accessing API endpoints, don't rewrite
						if strings.HasPrefix(originalPath, "/api/") {
							c.Next()
							return
						}
						
						// Remove leading slash from original path
						if originalPath == "/" {
							originalPath = ""
						} else if strings.HasPrefix(originalPath, "/") {
							originalPath = originalPath[1:]
						}
						
						// Ensure path starts with /
						if !strings.HasPrefix(path, "/") {
							path = "/" + path
						}
						
						// Construct new path
						var newPath string
						if path == "/" || path == "/public" {
							// Default hash subdomain points to /public
							if originalPath == "" {
								newPath = "/datasites/" + email + "/public/"
							} else {
								newPath = "/datasites/" + email + "/public/" + originalPath
							}
						} else {
							// Custom path case
							if originalPath == "" {
								newPath = "/datasites/" + email + path + "/"
							} else {
								newPath = "/datasites/" + email + path + "/" + originalPath
							}
						}
						c.Request.URL.Path = newPath
						
						// Store original path for logging/debugging
						c.Set("original_path", originalPath)
						c.Set("rewritten_path", newPath)
						
						c.Next()
						return
					}
				}
			}
		}

		// If we reach here, it's not a recognized domain
		c.Set(IsSubdomainRequestKey, false)

		c.Next()
	}
}

// isSubdomainRequest checks if the host is a subdomain of the main domain
func isSubdomainRequest(host, mainDomain string) bool {
	// Strip port if present
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}
	
	// Check if host ends with main domain
	if !strings.HasSuffix(host, mainDomain) {
		return false
	}

	// Check if there's a subdomain
	prefix := strings.TrimSuffix(host, "."+mainDomain)
	return prefix != "" && prefix != host && prefix != "www"
}

// extractSubdomain extracts the subdomain from the host
func extractSubdomain(host, mainDomain string) string {
	// Strip port if present
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}
	
	if !strings.HasSuffix(host, mainDomain) {
		return ""
	}
	
	prefix := strings.TrimSuffix(host, "."+mainDomain)
	if prefix == host {
		return ""
	}
	
	return prefix
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// EmailToSubdomainHash generates a subdomain-safe hash from an email address
func EmailToSubdomainHash(email string) string {
	// Normalize email to lowercase
	email = strings.ToLower(strings.TrimSpace(email))
	
	// Create SHA256 hash
	hash := sha256.Sum256([]byte(email))
	
	// Convert to hex and take first 16 characters for subdomain
	// This provides sufficient uniqueness while keeping subdomain length reasonable
	return hex.EncodeToString(hash[:])[:16]
}

// IsSubdomainRequest checks if the current request is a subdomain request
func IsSubdomainRequest(c *gin.Context) bool {
	isSubdomain, exists := c.Get(IsSubdomainRequestKey)
	if !exists {
		return false
	}
	return isSubdomain.(bool)
}

// GetSubdomainEmail retrieves the email associated with the subdomain
func GetSubdomainEmail(c *gin.Context) (string, bool) {
	email, exists := c.Get(SubdomainEmailKey)
	if !exists {
		return "", false
	}
	return email.(string), true
}

// GetSubdomainHash retrieves the subdomain hash from context
func GetSubdomainHash(c *gin.Context) (string, bool) {
	hash, exists := c.Get(SubdomainHashKey)
	if !exists {
		return "", false
	}
	return hash.(string), true
}