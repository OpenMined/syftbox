package middlewares

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/utils"
)

// KeySubdomainRequest indicates if this is a subdomain request
const KeySubdomainRequest = "subdomainRequest"

var (
	redirectNonce          = utils.TokenHex(32)
	headerInternalRedirect = "x-internal-redirect"
)

type SubdomainRouterConfig struct {
	Domain  string // base domain
	Mapping *datasite.SubdomainMapping
}

func SubdomainRewrite(e *gin.Engine, config *SubdomainRouterConfig) gin.HandlerFunc {
	if config.Domain == "" || config.Mapping == nil {
		slog.Info("subdomain routing disabled")
		return func(c *gin.Context) {
			// Continue to the next handler
			c.Next()
		}
	}

	slog.Info("subdomain routing enabled", "domain", config.Domain)

	return func(c *gin.Context) {
		host := c.Request.Host
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			// Remove port if present
			host = host[:idx]
		}

		// host is root domain
		if host == config.Domain {
			// Continue to the next handler
			c.Next()
			return
		}

		// this is the exit condition for the subdomain rewrite
		// if the request is a subdomain request, set the context and delete the headers
		if c.GetHeader(headerInternalRedirect) == redirectNonce {

			// set the context values
			c.Set(KeySubdomainRequest, true)

			// delete the header
			c.Request.Header.Del(headerInternalRedirect)

			// Continue to the next handler
			c.Next()
			return
		}

		// if this is a vanity domain then rewrite the path
		// can be custom domain or a hash-based subdomain
		if config, ok := config.Mapping.GetVanityDomain(host); ok {
			user := config.Email
			baseDir := config.Path

			// rewrite the path
			originalPath := c.Request.URL.Path
			newPath := sandboxedRewrite(originalPath, user, baseDir)

			slog.Info("rewriting path", "host", host, "original", originalPath, "new", newPath)

			// rewrite the path
			c.Request.URL.Path = newPath

			// using request headers instead because gin context is cleared in e.HandleContext
			// use a nonce to prevent malicious user attacks
			c.Request.Header.Set(headerInternalRedirect, redirectNonce)

			// re-enter the updated context
			e.HandleContext(c)
			return
		}

		// fallback check for local dev before erroring out
		if isLocalDevRequest(host) {
			// Continue to the next handler
			c.Next()
			return
		}

		// not a valid request
		abortWithInvalidSubdomain(c, host)
	}
}

func sandboxedRewrite(originalPath string, user string, baseDir string) string {
	// original path gets converted to the sandboxed
	// /datasites/{email}/{basePath}/{originalPath}

	// default to public directory
	if baseDir == "" || baseDir == "/" {
		baseDir = "public"
	}

	if originalPath == "/" {
		originalPath = ""
	}

	// cleanup paths
	baseDir = strings.TrimPrefix(baseDir, "/")
	baseDir = strings.TrimSuffix(baseDir, "/")
	originalPath = strings.TrimPrefix(originalPath, "/")

	// construct the new path
	// not using filepath.Join because it resolves relative paths like ".."
	// and we don't want to do that for security reasons
	return strings.Join([]string{"/datasites", user, baseDir, originalPath}, "/")
}

func abortWithInvalidSubdomain(c *gin.Context, host string) {
	c.Error(fmt.Errorf("invalid subdomain %s", host))
	api.ServeErrorHTML(c, http.StatusInternalServerError, "500 Internal Server Error", fmt.Sprintf("The subdomain <b><code>%s</code></b> is not available or has not been configured by the datasite owner.", host))
}

func isLocalDevRequest(host string) bool {
	return strings.Contains(host, "127.0.0.1") || strings.Contains(host, "0.0.0.0") || strings.Contains(host, "localhost")
}

func IsSubdomainRequest(c *gin.Context) bool {
	return c.GetBool(KeySubdomainRequest)
}
