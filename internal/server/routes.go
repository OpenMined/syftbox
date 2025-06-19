package server

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"

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

func SetupRoutes(svc *Services, hub *ws.WebsocketHub, httpsEnabled bool) http.Handler {
	r := gin.New()

	// --------------------------- middlewares ---------------------------

	r.Use(gin.Recovery())
	r.Use(middlewares.Logger())
	r.Use(middlewares.CORS())
	r.Use(middlewares.GZIP())
	if httpsEnabled {
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
		r.GET("/", IndexHandler)
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

	// enable auth middleware with guest access
	v1.Use(middlewares.JWTAuth(svc.Auth, true))
	{
		// send rpc routes
		v1.Any("/send/msg", sendH.SendMsg)
		v1.GET("/send/poll", sendH.PollForResponse)
	}

	r.NoRoute(func(c *gin.Context) {
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
