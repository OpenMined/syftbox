package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTokenAuth_Disabled_AllowsRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TokenAuth(TokenAuthConfig{Token: ""}))
	r.GET("/ok", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

func TestTokenAuth_Enabled_RejectsMissingOrBad(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TokenAuth(TokenAuthConfig{Token: "secret"}))
	r.GET("/ok", func(c *gin.Context) { c.String(200, "ok") })

	// Missing
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Bad header token
	req2 := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req2.Header.Set("Authorization", "Bearer nope")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestTokenAuth_Enabled_AllowsHeaderOrQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TokenAuth(TokenAuthConfig{Token: "secret"}))
	r.GET("/ok", func(c *gin.Context) {
		authenticated, _ := c.Get("authenticated")
		c.JSON(200, gin.H{"auth": authenticated})
	})

	// Header
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"auth":true`)

	// Query
	req2 := httptest.NewRequest(http.MethodGet, "/ok?token=secret", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), `"auth":true`)
}

