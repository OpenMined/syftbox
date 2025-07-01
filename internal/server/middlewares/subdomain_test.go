package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/stretchr/testify/assert"
)

func TestSubdomainRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		host           string
		path           string
		mainDomain     string
		vanityDomains  map[string]struct{ email, path string }
		expectedPath   string
		expectedStatus int
		isSubdomain    bool
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true, // API paths are not rewritten, but are subdomain requests
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true, // API paths are not rewritten, but are subdomain requests
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
		},
		{
			name:           "Unknown subdomain",
			host:           "unknown.syftbox.net",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusInternalServerError, // Should return error for unknown subdomain
			isSubdomain:    false,
		},
		{
			name:           "Unknown vanity domain",
			host:           "unknown.site",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusInternalServerError, // Should return error for unknown vanity domain
			isSubdomain:    false,
		},
		{
			name:           "Different domain entirely",
			host:           "example.com",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusInternalServerError, // Should return error for unknown domain
			isSubdomain:    false,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
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
			expectedStatus: http.StatusOK,
			isSubdomain:    true,
		},
		// Local development tests
		{
			name:           "Localhost request",
			host:           "localhost",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusOK,
			isSubdomain:    false,
		},
		{
			name:           "127.0.0.1 request",
			host:           "127.0.0.1",
			path:           "/index.html",
			mainDomain:     "syftbox.net",
			vanityDomains:  map[string]struct{ email, path string }{},
			expectedPath:   "/index.html",
			expectedStatus: http.StatusOK,
			isSubdomain:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			// Create subdomain mapping
			subdomainMapping := datasite.NewSubdomainMapping()
			for domain, config := range tt.vanityDomains {
				subdomainMapping.AddVanityDomain(domain, config.email, config.path)
			}

			config := &SubdomainRewriteConfig{
				Domain:  tt.mainDomain,
				Mapping: subdomainMapping,
			}

			router.Use(SubdomainRewrite(router, config))

			// Test handler
			router.GET("/*path", func(c *gin.Context) {
				actualPath := c.Request.URL.Path
				assert.Equal(t, tt.expectedPath, actualPath)

				if tt.isSubdomain {
					assert.True(t, IsSubdomainRequest(c))
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

func TestSubdomainContextSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// Create subdomain mapping
	subdomainMapping := datasite.NewSubdomainMapping()
	subdomainMapping.AddVanityDomain("alice.blog", "alice@example.com", "/blog")

	config := &SubdomainRewriteConfig{
		Domain:  "syftbox.net",
		Mapping: subdomainMapping,
	}

	router.Use(SubdomainRewrite(router, config))

	// Test handler that checks context values
	router.GET("/*path", func(c *gin.Context) {
		// Check subdomain context
		isSubdomain := IsSubdomainRequest(c)
		assert.True(t, isSubdomain)

		// Check path rewriting
		expectedPath := "/datasites/alice@example.com/blog/index.html"
		actualPath := c.Request.URL.Path
		assert.Equal(t, expectedPath, actualPath)

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

			// Create subdomain mapping
			subdomainMapping := datasite.NewSubdomainMapping()
			subdomainMapping.AddVanityDomain(tt.host, "alice@example.com", tt.vanityPath)

			config := &SubdomainRewriteConfig{
				Domain:  "syftbox.net",
				Mapping: subdomainMapping,
			}

			router.Use(SubdomainRewrite(router, config))

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

func TestSubdomainRewriteDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		domain       string
		mapping      *datasite.SubdomainMapping
		host         string
		path         string
		expectedPath string
	}{
		{
			name:         "No domain configured",
			domain:       "",
			mapping:      datasite.NewSubdomainMapping(),
			host:         "alice.blog",
			path:         "/index.html",
			expectedPath: "/index.html", // Should not be rewritten
		},
		{
			name:         "No mapping configured",
			domain:       "syftbox.net",
			mapping:      nil,
			host:         "alice.blog",
			path:         "/index.html",
			expectedPath: "/index.html", // Should not be rewritten
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			config := &SubdomainRewriteConfig{
				Domain:  tt.domain,
				Mapping: tt.mapping,
			}

			router.Use(SubdomainRewrite(router, config))

			// Test handler
			router.GET("/*path", func(c *gin.Context) {
				actualPath := c.Request.URL.Path
				assert.Equal(t, tt.expectedPath, actualPath)
				assert.False(t, IsSubdomainRequest(c))
				c.Status(http.StatusOK)
			})

			// Create request
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Host = tt.host

			// Perform request
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Check status
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestSandboxedRewrite(t *testing.T) {
	tests := []struct {
		name         string
		urlPath      string
		user         string
		baseDir      string
		expectedPath string
	}{
		{
			name:         "Simple path",
			urlPath:      "/index.html",
			user:         "alice@example.com",
			baseDir:      "/public",
			expectedPath: "/datasites/alice@example.com/public/index.html",
		},
		{
			name:         "Root path",
			urlPath:      "/",
			user:         "alice@example.com",
			baseDir:      "/public",
			expectedPath: "/datasites/alice@example.com/public/",
		},
		{
			name:         "Nested path",
			urlPath:      "/docs/readme.md",
			user:         "alice@example.com",
			baseDir:      "/public",
			expectedPath: "/datasites/alice@example.com/public/docs/readme.md",
		},
		{
			name:         "Empty base dir",
			urlPath:      "/index.html",
			user:         "alice@example.com",
			baseDir:      "/",
			expectedPath: "/datasites/alice@example.com/index.html",
		},
		{
			name:         "Empty url path",
			urlPath:      "/",
			user:         "alice@example.com",
			baseDir:      "/blog",
			expectedPath: "/datasites/alice@example.com/blog/",
		},
		{
			name:         "Both empty",
			urlPath:      "/",
			user:         "alice@example.com",
			baseDir:      "/",
			expectedPath: "/datasites/alice@example.com/",
		},
		{
			name:         "No leading slashes",
			urlPath:      "index.html",
			user:         "alice@example.com",
			baseDir:      "public",
			expectedPath: "/datasites/alice@example.com/public/index.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sandboxedRewrite(tt.urlPath, tt.user, tt.baseDir)
			assert.Equal(t, tt.expectedPath, result)
		})
	}
}
