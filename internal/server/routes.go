package server

import (
	"log/slog"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	slogGin "github.com/samber/slog-gin"

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

func SetupRoutes(svc *Services, hub *ws.WebsocketHub) http.Handler {
	r := gin.New()
	r.MaxMultipartMemory = 8 << 20 // 8 MiB

	blobH := blob.New(svc.Blob)
	dsH := datasite.New(svc.Datasite)
	explorerH := explorer.New(svc.Blob, svc.ACL)
	authH := auth.New(svc.Auth)
	sendH := send.NewSendHandler(hub, svc.Blob)

	httpLogger := slog.Default().WithGroup("http")
	r.Use(slogGin.NewWithConfig(httpLogger, slogGin.Config{
		DefaultLevel:      slog.LevelInfo,
		ClientErrorLevel:  slog.LevelWarn,
		ServerErrorLevel:  slog.LevelError,
		WithRequestID:     true,
		WithRequestHeader: true,
		WithTraceID:       true,
		WithSpanID:        true,
	}))
	r.Use(gin.Recovery())
	r.Use(gzip.Gzip(gzip.BestSpeed))
	r.Use(cors.Default())

	r.GET("/", IndexHandler)
	r.GET("/healthz", HealthHandler)
	r.GET("/install.sh", install.ServeSH)
	r.GET("/install.ps1", install.ServePS1)
	r.GET("/datasites/*filepath", explorerH.Handler)
	r.StaticFS("/releases", http.Dir("./releases"))

	r.POST("/auth/otp/request", authH.OTPRequest)
	r.POST("/auth/otp/verify", authH.OTPVerify)
	r.POST("/auth/refresh", authH.Refresh)

	v1 := r.Group("/api/v1")
	v1.Use(middlewares.JWTAuth(svc.Auth))
	{
		// blob
		v1.GET("/blob/list", blobH.ListObjects)
		v1.PUT("/blob/upload", blobH.Upload)
		v1.POST("/blob/upload/presigned", blobH.UploadPresigned)
		v1.POST("/blob/upload/multipart", blobH.UploadMultipart)
		v1.POST("/blob/upload/complete", blobH.UploadComplete)
		v1.POST("/blob/download", blobH.DownloadObjectsPresigned)
		v1.POST("/blob/delete", blobH.DeleteObjects)

		// datasite
		v1.GET("/datasite/view", dsH.GetView)
		// v1.POST("/datasite/download", ds.DownloadFiles)

		// websocket events
		v1.GET("/events", hub.WebsocketHandler)

		// send, receive http messages
		v1.Any("/message/send", sendH.HandleSendMessage)
		v1.GET("/message/get", sendH.HandleGetMessage)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "not found",
		})
	})

	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"error": "method not allowed",
		})
	})

	return r.Handler()
}

func IndexHandler(ctx *gin.Context) {
	// return a plaintext
	ctx.String(http.StatusOK, version.DetailedWithApp())
}

func HealthHandler(ctx *gin.Context) {
	ctx.PureJSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}
