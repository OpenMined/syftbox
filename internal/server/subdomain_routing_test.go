package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/middlewares"
)

func TestSubdomainPathRewriting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		originalPath string
		email        string
		vanityPath   string
		expectedPath string
	}{
		{
			name:         "Root path",
			originalPath: "/",
			email:        "alice@example.com",
			vanityPath:   "/public",
			expectedPath: "/datasites/alice@example.com/public/",
		},
		{
			name:         "File path",
			originalPath: "/index.html",
			email:        "alice@example.com",
			vanityPath:   "/public",
			expectedPath: "/datasites/alice@example.com/public/index.html",
		},
		{
			name:         "Nested path",
			originalPath: "/docs/readme.md",
			email:        "alice@example.com",
			vanityPath:   "/public",
			expectedPath: "/datasites/alice@example.com/public/docs/readme.md",
		},
		{
			name:         "API path not rewritten",
			originalPath: "/api/v1/status",
			email:        "alice@example.com",
			vanityPath:   "/public",
			expectedPath: "/api/v1/status",
		},
		{
			name:         "Vanity domain with custom path",
			originalPath: "/post.html",
			email:        "alice@example.com",
			vanityPath:   "/blog",
			expectedPath: "/datasites/alice@example.com/blog/post.html",
		},
		{
			name:         "Vanity domain pointing to root",
			originalPath: "/about.html",
			email:        "alice@example.com",
			vanityPath:   "/",
			expectedPath: "/datasites/alice@example.com/about.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			// Create subdomain middleware config
			config := middlewares.SubdomainConfig{
				MainDomain: "syftbox.net",
				GetVanityDomainFunc: func(domain string) (string, string, bool) {
					return tt.email, tt.vanityPath, true
				},
			}

			router.Use(middlewares.SubdomainMiddleware(config))

			// Test handler to capture the rewritten path
			router.GET("/*path", func(c *gin.Context) {
				actualPath := c.Request.URL.Path
				assert.Equal(t, tt.expectedPath, actualPath)
				c.Status(http.StatusOK)
			})

			// Create request with subdomain
			req := httptest.NewRequest("GET", tt.originalPath, nil)
			req.Host = "abc123.syftbox.net"

			// Perform request
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestSubdomainSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// Create subdomain middleware config
	config := middlewares.SubdomainConfig{
		MainDomain: "syftbox.net",
		GetVanityDomainFunc: func(domain string) (string, string, bool) {
			if domain == "abc123.syftbox.net" {
				return "alice@example.com", "/public", true
			}
			return "", "", false
		},
	}

	router.Use(middlewares.SubdomainMiddleware(config))
	router.Use(middlewares.CORS())
	router.Use(middlewares.SetSubdomainSecurityHeaders)

	// Test handler
	router.GET("/*path", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Test subdomain request
	req := httptest.NewRequest("GET", "/test.html", nil)
	req.Host = "abc123.syftbox.net"
	req.Header.Set("Origin", "https://abc123.syftbox.net")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Check security headers
	assert.Equal(t, "https://abc123.syftbox.net", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "SAMEORIGIN", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "1; mode=block", w.Header().Get("X-XSS-Protection"))
	assert.Equal(t, "same-origin", w.Header().Get("Referrer-Policy"))
	assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))

	// Test main domain request (no subdomain)
	req2 := httptest.NewRequest("GET", "/test.html", nil)
	req2.Host = "syftbox.net"

	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	// Should not have subdomain-specific headers
	assert.Empty(t, w2.Header().Get("X-Frame-Options"))
	assert.Empty(t, w2.Header().Get("X-XSS-Protection"))
	assert.Empty(t, w2.Header().Get("Referrer-Policy"))
}

func TestEndToEndSubdomainRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name               string
		host               string
		path               string
		vanityDomains      map[string]struct{ email, path string }
		expectedStatusCode int
		expectedContent    string
		checkHeaders       bool
	}{
		{
			name: "Hash subdomain serves file",
			host: "ff8d9819fc0e12bf.syftbox.net",
			path: "/test.txt",
			vanityDomains: map[string]struct{ email, path string }{
				"ff8d9819fc0e12bf.syftbox.net": {"alice@example.com", "/public"},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    "Test content for alice",
			checkHeaders:       true,
		},
		{
			name: "Vanity domain serves file",
			host: "alice.blog",
			path: "/post.html",
			vanityDomains: map[string]struct{ email, path string }{
				"alice.blog": {"alice@example.com", "/blog"},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    "Blog post content",
			checkHeaders:       true,
		},
		{
			name:               "Unknown subdomain returns 404",
			host:               "unknown.syftbox.net",
			path:               "/test.txt",
			vanityDomains:      map[string]struct{ email, path string }{},
			expectedStatusCode: http.StatusNotFound,
			checkHeaders:       false,
		},
		{
			name:               "Main domain serves normally",
			host:               "syftbox.net",
			path:               "/api/health",
			vanityDomains:      map[string]struct{ email, path string }{},
			expectedStatusCode: http.StatusOK,
			expectedContent:    "healthy",
			checkHeaders:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			// Configure subdomain middleware
			config := middlewares.SubdomainConfig{
				MainDomain: "syftbox.net",
				GetVanityDomainFunc: func(domain string) (string, string, bool) {
					if cfg, exists := tt.vanityDomains[domain]; exists {
						return cfg.email, cfg.path, true
					}
					return "", "", false
				},
			}

			router.Use(middlewares.SubdomainMiddleware(config))
			router.Use(middlewares.CORS())
			router.Use(middlewares.SetSubdomainSecurityHeaders)

			// Debug middleware to see what's happening
			router.Use(func(c *gin.Context) {
				fmt.Printf("DEBUG: Request path: %s, Host: %s\n", c.Request.URL.Path, c.Request.Host)
				c.Next()
			})

			// Catch all handler for rewritten paths
			router.NoRoute(func(c *gin.Context) {
				path := c.Request.URL.Path
				fmt.Printf("DEBUG: NoRoute handler received path: %s\n", path)

				// Route based on the exact rewritten path
				switch path {
				case "/datasites/alice@example.com/public/test.txt":
					c.String(http.StatusOK, "Test content for alice")
				case "/datasites/alice@example.com/blog/post.html":
					c.String(http.StatusOK, "Blog post content")
				default:
					fmt.Printf("DEBUG: No match for path: %s\n", path)
					c.String(http.StatusNotFound, "404 page not found")
				}
			})

			router.GET("/api/health", func(c *gin.Context) {
				c.String(http.StatusOK, "healthy")
			})

			// Create request
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Host = tt.host

			// Perform request
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, w.Code)

			// Check content if expected
			if tt.expectedContent != "" {
				assert.Equal(t, tt.expectedContent, w.Body.String())
			}

			// Check security headers for subdomain requests
			if tt.checkHeaders && w.Code == http.StatusOK {
				assert.NotEmpty(t, w.Header().Get("X-Frame-Options"))
				assert.NotEmpty(t, w.Header().Get("X-Content-Type-Options"))
			}
		})
	}
}

func TestIndexHTMLAutoServing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// Configure subdomain middleware
	config := middlewares.SubdomainConfig{
		MainDomain: "syftbox.net",
		GetVanityDomainFunc: func(domain string) (string, string, bool) {
			if domain == "ff8d9819fc0e12bf.syftbox.net" {
				return "alice@example.com", "/public", true
			}
			return "", "", false
		},
	}

	router.Use(middlewares.SubdomainMiddleware(config))

	// Debug middleware
	router.Use(func(c *gin.Context) {
		fmt.Printf("DEBUG Index: Request path: %s, Host: %s\n", c.Request.URL.Path, c.Request.Host)
		c.Next()
	})

	// NoRoute handler to catch rewritten paths
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		fmt.Printf("DEBUG Index NoRoute: %s\n", path)

		// Extract the path after /datasites/alice@example.com
		if strings.HasPrefix(path, "/datasites/alice@example.com") {
			relativePath := strings.TrimPrefix(path, "/datasites/alice@example.com")

			// If path ends with /, serve index.html
			if strings.HasSuffix(relativePath, "/") {
				c.String(http.StatusOK, fmt.Sprintf("index.html content for %s", relativePath))
			} else if strings.HasSuffix(relativePath, "/index.html") {
				// Extract directory path for index.html
				dirPath := strings.TrimSuffix(relativePath, "index.html")
				c.String(http.StatusOK, fmt.Sprintf("index.html content for %s", dirPath))
			} else {
				// Serve regular file
				c.String(http.StatusOK, fmt.Sprintf("file content for %s", relativePath))
			}
		} else {
			c.String(http.StatusNotFound, "404 page not found")
		}
	})

	tests := []struct {
		name            string
		path            string
		expectedContent string
	}{
		{
			name:            "Root directory serves index.html",
			path:            "/",
			expectedContent: "index.html content for /public/",
		},
		{
			name:            "Subdirectory serves index.html",
			path:            "/docs/",
			expectedContent: "index.html content for /public/docs/",
		},
		{
			name:            "Direct index.html request",
			path:            "/index.html",
			expectedContent: "index.html content for /public/",
		},
		{
			name:            "Regular file request",
			path:            "/readme.md",
			expectedContent: "file content for /public/readme.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Host = "ff8d9819fc0e12bf.syftbox.net"

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tt.expectedContent, w.Body.String())
		})
	}
}

func TestRelativeLinkGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// Configure subdomain middleware
	config := middlewares.SubdomainConfig{
		MainDomain: "syftbox.net",
		GetVanityDomainFunc: func(domain string) (string, string, bool) {
			if domain == "alice.blog" {
				return "alice@example.com", "/blog", true
			}
			return "", "", false
		},
	}

	router.Use(middlewares.SubdomainMiddleware(config))

	// NoRoute handler for rewritten paths
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Check if it's a datasite path
		if strings.HasPrefix(path, "/datasites/alice@example.com/blog/") {
			// Generate a simple directory listing with relative links
			html := `<html><body><ul>`
			html += `<li><a href="post1.html">Post 1</a></li>`
			html += `<li><a href="post2.html">Post 2</a></li>`
			html += `<li><a href="images/">Images Directory</a></li>`
			html += `<li><a href="../">Parent Directory</a></li>`
			html += `</ul></body></html>`

			c.Header("Content-Type", "text/html")
			c.String(http.StatusOK, html)
		} else {
			c.String(http.StatusNotFound, "404 page not found")
		}
	})

	// Request directory listing
	req := httptest.NewRequest("GET", "/posts/", nil)
	req.Host = "alice.blog"

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `href="post1.html"`)
	assert.Contains(t, w.Body.String(), `href="post2.html"`)
	assert.Contains(t, w.Body.String(), `href="images/"`)
	assert.Contains(t, w.Body.String(), `href="../"`)
}

func TestACLEnforcementForSubdomains(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Mock ACL checker
	aclChecker := func(email, path, requester string) bool {
		// Public files are accessible to everyone
		if strings.Contains(path, "/public/") {
			return true
		}
		// Alice can access her own files
		if email == "alice@example.com" && requester == "alice@example.com" {
			return true
		}
		// Bob cannot access Alice's private files
		if email == "alice@example.com" && requester == "bob@example.com" {
			return false
		}
		return false
	}

	router := gin.New()

	// Configure subdomain middleware
	config := middlewares.SubdomainConfig{
		MainDomain: "syftbox.net",
		GetVanityDomainFunc: func(domain string) (string, string, bool) {
			if domain == "ff8d9819fc0e12bf.syftbox.net" {
				return "alice@example.com", "/public", true
			}
			if domain == "alice.private" {
				return "alice@example.com", "/private", true
			}
			return "", "", false
		},
	}

	router.Use(middlewares.SubdomainMiddleware(config))

	// Mock ACL middleware
	router.Use(func(c *gin.Context) {
		// Extract subdomain email
		email, _ := middlewares.GetSubdomainEmail(c)
		path := c.Request.URL.Path

		// Get requester from header (in real app, from JWT)
		requester := c.GetHeader("X-Requester")

		if email != "" && !aclChecker(email, path, requester) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
	})

	// NoRoute handler for rewritten paths (Gin can't handle @ in route parameters)
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Check if it's a datasite path that should return content
		if strings.HasPrefix(path, "/datasites/alice@example.com/") {
			c.String(http.StatusOK, "File content")
		} else {
			c.String(http.StatusNotFound, "404 page not found")
		}
	})

	tests := []struct {
		name               string
		host               string
		path               string
		requester          string
		expectedStatusCode int
	}{
		{
			name:               "Alice accesses her public files",
			host:               "ff8d9819fc0e12bf.syftbox.net",
			path:               "/test.txt",
			requester:          "alice@example.com",
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "Bob accesses Alice's public files",
			host:               "ff8d9819fc0e12bf.syftbox.net",
			path:               "/test.txt",
			requester:          "bob@example.com",
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "Alice accesses her private files",
			host:               "alice.private",
			path:               "/secret.txt",
			requester:          "alice@example.com",
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "Bob cannot access Alice's private files",
			host:               "alice.private",
			path:               "/secret.txt",
			requester:          "bob@example.com",
			expectedStatusCode: http.StatusForbidden,
		},
		{
			name:               "Anonymous access to public files",
			host:               "ff8d9819fc0e12bf.syftbox.net",
			path:               "/test.txt",
			requester:          "",
			expectedStatusCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Host = tt.host
			if tt.requester != "" {
				req.Header.Set("X-Requester", tt.requester)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatusCode, w.Code)
		})
	}
}

func TestVanityDomainPathMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Test different vanity domain configurations
	vanityConfigs := map[string]struct{ email, path string }{
		"alice.blog":         {"alice@example.com", "/blog"},
		"alice.portfolio":    {"alice@example.com", "/portfolio"},
		"projects.alice.dev": {"alice@example.com", "/projects/2024"},
		"alice.site":         {"alice@example.com", "/"}, // Points to root
	}

	router := gin.New()

	// Configure subdomain middleware
	config := middlewares.SubdomainConfig{
		MainDomain: "syftbox.net",
		GetVanityDomainFunc: func(domain string) (string, string, bool) {
			if cfg, exists := vanityConfigs[domain]; exists {
				return cfg.email, cfg.path, true
			}
			return "", "", false
		},
	}

	router.Use(middlewares.SubdomainMiddleware(config))

	// NoRoute handler that echoes the path (Gin can't handle @ in route parameters)
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// For vanity domain path mapping test, echo back the rewritten path
		if strings.HasPrefix(path, "/datasites/alice@example.com/") {
			c.String(http.StatusOK, path)
		} else {
			c.String(http.StatusNotFound, "404 page not found")
		}
	})

	tests := []struct {
		name         string
		host         string
		requestPath  string
		expectedPath string
	}{
		{
			name:         "Blog domain root",
			host:         "alice.blog",
			requestPath:  "/",
			expectedPath: "/datasites/alice@example.com/blog/",
		},
		{
			name:         "Blog domain with file",
			host:         "alice.blog",
			requestPath:  "/post1.html",
			expectedPath: "/datasites/alice@example.com/blog/post1.html",
		},
		{
			name:         "Portfolio domain",
			host:         "alice.portfolio",
			requestPath:  "/project1/index.html",
			expectedPath: "/datasites/alice@example.com/portfolio/project1/index.html",
		},
		{
			name:         "Nested vanity path",
			host:         "projects.alice.dev",
			requestPath:  "/demo/app.js",
			expectedPath: "/datasites/alice@example.com/projects/2024/demo/app.js",
		},
		{
			name:         "Root vanity domain",
			host:         "alice.site",
			requestPath:  "/about.html",
			expectedPath: "/datasites/alice@example.com/about.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.requestPath, nil)
			req.Host = tt.host

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tt.expectedPath, w.Body.String())
		})
	}
}

func TestHashAndVanityDomainCoexistence(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()

	aliceHash := datasite.EmailToSubdomainHash("alice@example.com")

	// Configure both hash and vanity domains for same user
	vanityConfigs := map[string]struct{ email, path string }{
		aliceHash + ".syftbox.net": {"alice@example.com", "/public"},
		"alice.blog":               {"alice@example.com", "/blog"},
		"alice.dev":                {"alice@example.com", "/dev"},
	}

	config := middlewares.SubdomainConfig{
		MainDomain: "syftbox.net",
		GetVanityDomainFunc: func(domain string) (string, string, bool) {
			if cfg, exists := vanityConfigs[domain]; exists {
				return cfg.email, cfg.path, true
			}
			return "", "", false
		},
	}

	router.Use(middlewares.SubdomainMiddleware(config))

	// NoRoute handler that returns the domain type (Gin can't handle @ in route parameters)
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Check if it's a datasite path
		if strings.HasPrefix(path, "/datasites/alice@example.com/") {
			isVanity, _ := c.Get("is_vanity_domain")
			vanityDomain, _ := c.Get("vanity_domain")

			response := fmt.Sprintf("Path: %s, IsVanity: %v, Domain: %v",
				path, isVanity, vanityDomain)
			c.String(http.StatusOK, response)
		} else {
			c.String(http.StatusNotFound, "404 page not found")
		}
	})

	tests := []struct {
		name           string
		host           string
		expectedDomain string
	}{
		{
			name:           "Hash subdomain",
			host:           aliceHash + ".syftbox.net",
			expectedDomain: aliceHash + ".syftbox.net",
		},
		{
			name:           "Blog vanity domain",
			host:           "alice.blog",
			expectedDomain: "alice.blog",
		},
		{
			name:           "Dev vanity domain",
			host:           "alice.dev",
			expectedDomain: "alice.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test.html", nil)
			req.Host = tt.host

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), "IsVanity: true")
			assert.Contains(t, w.Body.String(), fmt.Sprintf("Domain: %s", tt.expectedDomain))
		})
	}
}
