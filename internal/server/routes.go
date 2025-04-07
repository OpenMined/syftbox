package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/blob"
	"github.com/yashgorana/syftbox-go/internal/datasite"
	"github.com/yashgorana/syftbox-go/internal/server/middlewares"
	blobHandler "github.com/yashgorana/syftbox-go/internal/server/v1/blob"
	datasiteHandler "github.com/yashgorana/syftbox-go/internal/server/v1/datasite"
	wsV1 "github.com/yashgorana/syftbox-go/internal/server/v1/ws"
	"github.com/yashgorana/syftbox-go/internal/version"

	_ "embed"
)

//go:embed templates/install.sh
var installScript string

func SetupRoutes(hub *wsV1.WebsocketHub, svcBlob *blob.BlobService, svcDatasite *datasite.DatasiteService) http.Handler {
	r := gin.Default()

	blob := blobHandler.New(svcBlob)
	ds := datasiteHandler.New(svcDatasite)

	r.Use(gzip.Gzip(gzip.BestSpeed))
	r.Use(cors.Default())

	r.GET("/", IndexHandler)
	r.GET("/healthz", HealthHandler)
	r.GET("/install.sh", InstallHeader)
	r.StaticFS("/releases", http.Dir("./releases"))

	v1 := r.Group("/api/v1")
	v1.Use(middlewares.Auth())
	{
		// blob
		v1.GET("/blob/list", blob.ListObjects)
		v1.PUT("/blob/upload", blob.Upload)
		v1.POST("/blob/upload/presigned", blob.UploadPresigned)
		v1.POST("/blob/upload/multipart", blob.UploadMultipart)
		v1.POST("/blob/upload/complete", blob.UploadComplete)
		v1.POST("/blob/download", blob.DownloadObjectsPresigned)
		v1.POST("/blob/delete", blob.DeleteObjects)

		// datasite
		v1.GET("/datasite/view", ds.GetView)
		// v1.POST("/datasite/download", ds.DownloadFiles)

		// websocket events
		v1.GET("/events", hub.WebsocketHandler)
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

func InstallHeader(ctx *gin.Context) {
	ctx.Header("Content-Type", "application/x-sh")
	ctx.Header("Content-Disposition", "attachment; filename=install.sh")
	ctx.String(http.StatusOK, installScript)
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}
