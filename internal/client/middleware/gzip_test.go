package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGzip_CompressesNonExcludedPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Gzip())
	r.GET("/ok", func(c *gin.Context) {
		c.String(200, strings.Repeat("x", 2048))
	})
	r.GET("/health", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// Non-excluded should gzip when requested.
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))

	// Excluded path should not gzip even if requested.
	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Empty(t, w2.Header().Get("Content-Encoding"))
}

