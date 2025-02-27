package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/pkg/blob"
	"github.com/yashgorana/syftbox-go/pkg/datasite"
	blobV1 "github.com/yashgorana/syftbox-go/pkg/server/v1/blob"
	datasiteV1 "github.com/yashgorana/syftbox-go/pkg/server/v1/datasite"
	wsV1 "github.com/yashgorana/syftbox-go/pkg/server/v1/ws"
)

func SetupRoutes(hub *wsV1.WebsocketHub, svcBlob *blob.BlobService, svcDatasite *datasite.DatasiteService) http.Handler {
	r := gin.Default()

	blob := blobV1.NewHandler(svcBlob)
	ds := datasiteV1.NewHandler(svcDatasite)

	r.Use(gzip.Gzip(gzip.BestSpeed))
	r.Use(cors.Default())

	r.GET("/", IndexHandler)
	r.GET("/healthz", HealthHandler)

	v1 := r.Group("/api/v1")
	{
		// blob
		v1.Any("/blob/list", blob.List)
		v1.Any("/blob/upload", blob.Upload)
		v1.Any("/blob/download", blob.Download)
		v1.Any("/blob/complete", blob.Complete)

		// datasite
		v1.Any("/datasite/view", ds.GetView)

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
	ctx.String(http.StatusOK, "SyftBox GO")
}

func HealthHandler(ctx *gin.Context) {
	ctx.PureJSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}
