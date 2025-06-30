package middlewares

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSubdomainMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		host           string
		path           string
		mainDomain     string
		vanityDomains  map[string]struct{ email, path string }
		expectedPath   string
		expectedEmail  string
		expectedStatus int
		isSubdomain    bool
		isVanity       bool
	}{
		// Hash subdomain tests
		{
			name:       "Hash subdomain with file",
			host:       "ff8d9819fc0e12bf.syftbox.net",
			path:       "/index.html",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"ff8d9819fc0e12bf.syftbox.net": {"alice@example.com", "/public"},
			},
			expectedPath:   "/datasites/alice@example.com/public/index.html",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "Hash subdomain with root path",
			host:       "ff8d9819fc0e12bf.syftbox.net",
			path:       "/",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"ff8d9819fc0e12bf.syftbox.net": {"alice@example.com", "/public"},
			},
			expectedPath:   "/datasites/alice@example.com/public/",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "Hash subdomain with nested path",
			host:       "ff8d9819fc0e12bf.syftbox.net",
			path:       "/docs/readme.md",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"ff8d9819fc0e12bf.syftbox.net": {"alice@example.com", "/public"},
			},
			expectedPath:   "/datasites/alice@example.com/public/docs/readme.md",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		// Vanity domain tests
		{
			name:       "Vanity domain with custom path",
			host:       "alice.blog",
			path:       "/post.html",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.blog": {"alice@example.com", "/blog"},
			},
			expectedPath:   "/datasites/alice@example.com/blog/post.html",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "Vanity domain with root path pointing to subdirectory",
			host:       "alice.blog",
			path:       "/",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.blog": {"alice@example.com", "/blog"},
			},
			expectedPath:   "/datasites/alice@example.com/blog/",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "Vanity domain with nested custom path",
			host:       "projects.alice.dev",
			path:       "/demo/index.html",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"projects.alice.dev": {"alice@example.com", "/projects/2024"},
			},
			expectedPath:   "/datasites/alice@example.com/projects/2024/demo/index.html",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "Vanity domain pointing to root",
			host:       "alice.site",
			path:       "/about.html",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.site": {"alice@example.com", "/"},
			},
			expectedPath:   "/datasites/alice@example.com/about.html",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		// API endpoint tests
		{
			name:       "API endpoint on hash subdomain",
			host:       "ff8d9819fc0e12bf.syftbox.net",
			path:       "/api/v1/status",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"ff8d9819fc0e12bf.syftbox.net": {"alice@example.com", "/public"},
			},
			expectedPath:   "/api/v1/status", // Should not be rewritten
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "API endpoint on vanity domain",
			host:       "alice.blog",
			path:       "/api/v1/posts",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.blog": {"alice@example.com", "/blog"},
			},
			expectedPath:   "/api/v1/posts", // Should not be rewritten
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		// Non-subdomain tests
		{
			name:           "Main domain request",
			host:           "syftbox.net",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusOK,
			isSubdomain:    false,
			isVanity:       false,
		},
		{
			name:           "Unknown subdomain",
			host:           "unknown.syftbox.net",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusOK,
			isSubdomain:    false,
			isVanity:       false,
		},
		{
			name:           "Unknown vanity domain",
			host:           "unknown.site",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusOK,
			isSubdomain:    false,
			isVanity:       false,
		},
		{
			name:           "Different domain entirely",
			host:           "example.com",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusOK,
			isSubdomain:    false,
			isVanity:       false,
		},
		// Port handling
		{
			name:       "Subdomain with port",
			host:       "ff8d9819fc0e12bf.syftbox.net:8080",
			path:       "/index.html",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"ff8d9819fc0e12bf.syftbox.net": {"alice@example.com", "/public"},
			},
			expectedPath:   "/datasites/alice@example.com/public/index.html",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "Vanity domain with port",
			host:       "alice.blog:3000",
			path:       "/post.html",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.blog": {"alice@example.com", "/blog"},
			},
			expectedPath:   "/datasites/alice@example.com/blog/post.html",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		// Edge cases
		{
			name:       "Empty path on vanity domain",
			host:       "alice.blog",
			path:       "/",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.blog": {"alice@example.com", "/blog"},
			},
			expectedPath:   "/datasites/alice@example.com/blog/",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
		{
			name:       "Double slash prevention",
			host:       "alice.blog",
			path:       "//double//slash//",
			mainDomain: "syftbox.net",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.blog": {"alice@example.com", "/blog"},
			},
			expectedPath:   "/datasites/alice@example.com/blog//double//slash//",
			expectedEmail:  "alice@example.com",
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
			isVanity:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			// Mock vanity domain function
			vanityDomainFunc := func(domain string) (string, string, bool) {
				if config, exists := tt.vanityDomains[domain]; exists {
					return config.email, config.path, true
				}
				return "", "", false
			}

			config := SubdomainConfig{
				MainDomain:          tt.mainDomain,
				GetVanityDomainFunc: vanityDomainFunc,
			}

			router.Use(SubdomainMiddleware(config))

			// Test handler
			router.GET("/*path", func(c *gin.Context) {
				actualPath := c.Request.URL.Path
				assert.Equal(t, tt.expectedPath, actualPath)

				if tt.isSubdomain {
					assert.True(t, IsSubdomainRequest(c))
					if tt.expectedEmail != "" {
						email, exists := GetSubdomainEmail(c)
						assert.True(t, exists)
						assert.Equal(t, tt.expectedEmail, email)
					}

					// Check vanity domain flag
					isVanity, _ := c.Get("is_vanity_domain")
					if tt.isVanity {
						assert.True(t, isVanity.(bool))
						vanityDomain, _ := c.Get("vanity_domain")
						// Remove port for comparison
						expectedDomain := tt.host
						if idx := strings.LastIndex(expectedDomain, ":"); idx != -1 {
							expectedDomain = expectedDomain[:idx]
						}
						assert.Equal(t, expectedDomain, vanityDomain)
					}
				} else {
					assert.False(t, IsSubdomainRequest(c))
				}

				c.Status(http.StatusOK)
			})

			// Create request
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Host = tt.host

			// Perform request
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Check status
			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestExtractSubdomain(t *testing.T) {
	tests := []struct {
		host       string
		mainDomain string
		expected   string
	}{
		{"abc123.syftbox.net", "syftbox.net", "abc123"},
		{"www.syftbox.net", "syftbox.net", "www"},
		{"syftbox.net", "syftbox.net", ""},
		{"example.com", "syftbox.net", ""},
		{"deep.sub.syftbox.net", "syftbox.net", "deep.sub"},
		{"", "syftbox.net", ""},
		{"abc123.syftbox.net:8080", "syftbox.net", "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result := extractSubdomain(tt.host, tt.mainDomain)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSubdomainRequest(t *testing.T) {
	tests := []struct {
		host       string
		mainDomain string
		expected   bool
	}{
		{"abc123.syftbox.net", "syftbox.net", true},
		{"www.syftbox.net", "syftbox.net", false}, // www is excluded
		{"syftbox.net", "syftbox.net", false},
		{"example.com", "syftbox.net", false},
		{"", "syftbox.net", false},
		{"deep.sub.syftbox.net", "syftbox.net", true},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result := isSubdomainRequest(tt.host, tt.mainDomain)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubdomainContextSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// Mock vanity domain function
	vanityDomainFunc := func(domain string) (string, string, bool) {
		if domain == "alice.blog" {
			return "alice@example.com", "/blog", true
		}
		return "", "", false
	}

	config := SubdomainConfig{
		MainDomain:          "syftbox.net",
		GetVanityDomainFunc: vanityDomainFunc,
	}

	router.Use(SubdomainMiddleware(config))

	// Test handler that checks context values
	router.GET("/*path", func(c *gin.Context) {
		// Check subdomain context
		isSubdomain := IsSubdomainRequest(c)
		assert.True(t, isSubdomain)

		// Check email
		email, exists := GetSubdomainEmail(c)
		assert.True(t, exists)
		assert.Equal(t, "alice@example.com", email)

		// Check vanity domain flag
		isVanity, exists := c.Get("is_vanity_domain")
		assert.True(t, exists)
		assert.True(t, isVanity.(bool))

		// Check vanity domain
		vanityDomain, exists := c.Get("vanity_domain")
		assert.True(t, exists)
		assert.Equal(t, "alice.blog", vanityDomain)

		// Check path rewriting
		originalPath, exists := c.Get("original_path")
		assert.True(t, exists)
		assert.Equal(t, "index.html", originalPath)

		rewrittenPath, exists := c.Get("rewritten_path")
		assert.True(t, exists)
		assert.Equal(t, "/datasites/alice@example.com/blog/index.html", rewrittenPath)

		c.Status(http.StatusOK)
	})

	// Create request
	req := httptest.NewRequest("GET", "/index.html", nil)
	req.Host = "alice.blog"

	// Perform request
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Check status
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPathRewritingEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		host         string
		originalPath string
		vanityPath   string
		expectedPath string
	}{
		{
			name:         "Root to root",
			host:         "alice.site",
			originalPath: "/",
			vanityPath:   "/",
			expectedPath: "/datasites/alice@example.com/",
		},
		{
			name:         "File at root",
			host:         "alice.site",
			originalPath: "/index.html",
			vanityPath:   "/",
			expectedPath: "/datasites/alice@example.com/index.html",
		},
		{
			name:         "Root to subdirectory",
			host:         "alice.blog",
			originalPath: "/",
			vanityPath:   "/blog",
			expectedPath: "/datasites/alice@example.com/blog/",
		},
		{
			name:         "File in subdirectory",
			host:         "alice.blog",
			originalPath: "/post.html",
			vanityPath:   "/blog",
			expectedPath: "/datasites/alice@example.com/blog/post.html",
		},
		{
			name:         "Nested path",
			host:         "alice.projects",
			originalPath: "/2024/demo/index.html",
			vanityPath:   "/projects",
			expectedPath: "/datasites/alice@example.com/projects/2024/demo/index.html",
		},
		{
			name:         "Empty original path",
			host:         "alice.blog",
			originalPath: "/",
			vanityPath:   "/blog",
			expectedPath: "/datasites/alice@example.com/blog/",
		},
		{
			name:         "Path without leading slash",
			host:         "alice.blog",
			originalPath: "/post.html",
			vanityPath:   "/blog",
			expectedPath: "/datasites/alice@example.com/blog/post.html",
		},
		{
			name:         "Vanity path without leading slash",
			host:         "alice.blog",
			originalPath: "/post.html",
			vanityPath:   "blog",
			expectedPath: "/datasites/alice@example.com/blog/post.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			// Mock vanity domain function
			vanityDomainFunc := func(domain string) (string, string, bool) {
				if domain == tt.host {
					return "alice@example.com", tt.vanityPath, true
				}
				return "", "", false
			}

			config := SubdomainConfig{
				MainDomain:          "syftbox.net",
				GetVanityDomainFunc: vanityDomainFunc,
			}

			router.Use(SubdomainMiddleware(config))

			// Test handler
			router.GET("/*path", func(c *gin.Context) {
				actualPath := c.Request.URL.Path
				assert.Equal(t, tt.expectedPath, actualPath)
				c.Status(http.StatusOK)
			})

			// Create request
			req := httptest.NewRequest("GET", tt.originalPath, nil)
			req.Host = tt.host

			// Perform request
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Check status
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}
