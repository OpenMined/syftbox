package server

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/openmined/syftbox/internal/server/handlers/acl"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/server/handlers/auth"
	"github.com/openmined/syftbox/internal/server/handlers/blob"
	"github.com/openmined/syftbox/internal/server/handlers/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/explorer"
	"github.com/openmined/syftbox/internal/server/handlers/install"
	"github.com/openmined/syftbox/internal/server/handlers/send"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/server/middlewares"
	"github.com/openmined/syftbox/internal/version"
)

//go:embed handlers/send/*.html
var templateFS embed.FS

func SetupRoutes(cfg *Config, svc *Services, hub *ws.WebsocketHub) http.Handler {
	r := gin.New()

	// --------------------------- middlewares ---------------------------

	r.Use(gin.Recovery())
	r.Use(middlewares.Logger())
	
	// Add subdomain middleware if domain is configured
	if cfg.HTTP.Domain != "" {
		subdomainMapping := svc.Datasite.GetSubdomainMapping()
		subdomainConfig := middlewares.SubdomainConfig{
			MainDomain: cfg.HTTP.Domain,
			GetVanityDomainFunc: func(domain string) (email string, path string, exists bool) {
				// First check if it's a vanity domain
				if config, ok := subdomainMapping.GetVanityDomain(domain); ok {
					return config.Email, config.Path, true
				}
				
				// Check if it's a hash subdomain (e.g., ff8d9819fc0e12bf.syftbox.local)
				if idx := strings.Index(domain, "."); idx > 0 && idx == 16 {
					hash := domain[:idx]
					// Check if this is a valid hash subdomain
					if email, err := subdomainMapping.GetEmailByHash(hash); err == nil {
						// Hash subdomains default to /public path
						return email, "/public", true
					}
				}
				
				return "", "", false
			},
		}
		r.Use(middlewares.SubdomainMiddleware(subdomainConfig))
	}
	
	r.Use(middlewares.CORS())
	r.Use(middlewares.SetSubdomainSecurityHeaders)
	r.Use(middlewares.GZIP())
	if cfg.HTTP.HTTPSEnabled() {
		r.Use(middlewares.HSTS())
	}

	// Load HTML templates from embedded filesystem
	tmpl := template.Must(template.ParseFS(templateFS, "handlers/send/*.html"))
	r.SetHTMLTemplate(tmpl)

	// --------------------------- handlers ---------------------------

	blobH := blob.New(svc.Blob, svc.ACL)
	dsH := datasite.New(svc.Datasite)
	explorerH := explorer.New(svc.Blob, svc.ACL)
	authH := auth.New(svc.Auth)
	aclH := acl.NewACLHandler(svc.ACL)
	sendH := send.New(send.NewWSMsgDispatcher(hub), send.NewBlobMsgStore(svc.Blob))

	// --------------------------- routes ---------------------------

	if os.Getenv("SYFTBOX_REDIRECT_WWW") == "1" {
		r.GET("/", IndexRedirectHandler)
	} else {
		r.GET("/", func(c *gin.Context) {
			// Check if this is a subdomain request
			if isSubdomain, _ := c.Get(middlewares.IsSubdomainRequestKey); isSubdomain == true {
				// Serve the explorer for subdomain root
				explorerH.Handler(c)
				return
			}
			IndexHandler(c)
		})
	}
	r.GET("/healthz", HealthHandler)
	r.GET("/install.sh", install.ServeSH)
	r.GET("/install.ps1", install.ServePS1)
	r.GET("/datasites/*filepath", explorerH.Handler)
	r.StaticFS("/releases", http.Dir("./releases"))

	auth := r.Group("/auth")
	auth.Use(middlewares.RateLimiter("10-M")) // 10 req/min
	{
		auth.POST("/otp/request", authH.OTPRequest)
		auth.POST("/otp/verify", authH.OTPVerify)
		auth.POST("/refresh", authH.Refresh)
	}

	v1 := r.Group("/api/v1")

	// enable auth middleware with no guest access
	v1.Use(middlewares.JWTAuth(svc.Auth, false))
	// v1.Use(middlewares.RateLimiter("100-S")) // todo
	{
		// blob
		v1.GET("/blob/list", blobH.ListObjects)
		v1.PUT("/blob/upload", blobH.Upload)
		v1.PUT("/blob/upload/acl", blobH.UploadACL)
		v1.POST("/blob/upload/presigned", blobH.UploadPresigned)
		v1.POST("/blob/upload/multipart", blobH.UploadMultipart)
		v1.POST("/blob/upload/complete", blobH.UploadComplete)
		v1.POST("/blob/download", blobH.DownloadObjectsPresigned)
		v1.POST("/blob/delete", blobH.DeleteObjects)

		// datasite
		v1.GET("/datasite/view", dsH.GetView)

		v1.PUT("/acl", blobH.UploadACL)
		v1.GET("/acl/check", aclH.CheckAccess)

		// websocket events
		v1.GET("/events", hub.WebsocketHandler)

	}

	// rpc group with guest access
	sendG := r.Group("/api/v1/send")
	sendG.Use(middlewares.JWTAuth(svc.Auth, true))
	{
		sendG.Any("/msg", sendH.SendMsg)
		sendG.GET("/poll", sendH.PollForResponse)
	}

	r.NoRoute(func(c *gin.Context) {
		// Check if this is a subdomain request that got rewritten
		if isSubdomain, _ := c.Get(middlewares.IsSubdomainRequestKey); isSubdomain == true {
			// The path was already rewritten by middleware, serve it with explorer
			explorerH.Handler(c)
			return
		}
		
		// Not a subdomain request, return 404
		c.JSON(http.StatusNotFound, api.SyftAPIError{
			Code:    api.CodeInvalidRequest,
			Message: "not found",
		})
	})

	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, api.SyftAPIError{
			Code:    api.CodeInvalidRequest,
			Message: "method not allowed",
		})
	})

	return r.Handler()
}

func IndexHandler(ctx *gin.Context) {
	// return a plaintext
	ctx.String(http.StatusOK, version.DetailedWithApp())
}

func IndexRedirectHandler(ctx *gin.Context) {
	host := ctx.Request.Host
	redirect := fmt.Sprintf("https://www.%s", host)
	ctx.Redirect(http.StatusTemporaryRedirect, redirect)
}

func HealthHandler(ctx *gin.Context) {
	ctx.PureJSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}
