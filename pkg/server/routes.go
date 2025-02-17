package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/pkg/datasite"
)

func SetupRoutes(datasiteSvc *datasite.DatasiteService) http.Handler {
	r := gin.Default()

	r.Use(gin.Recovery())
	r.Use(gzip.Gzip(gzip.BestCompression))
	r.Use(cors.Default())

	r.GET("/", IndexHandler)
	r.GET("/health", HealthHandler)

	// blob := NewBlobHandler(blobSvc)
	// r.GET("/blob/list", blob.List)
	// r.GET("/blob/upload", blob.Upload)
	// r.POST("/blob/complete", blob.Complete)

	ds := NewDatasiteHandler(datasiteSvc)
	r.GET("/datasite/view", ds.GetView)

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, ErrResponseNotFound)
	})

	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, ErrResponseMethodNotAllowed)
	})

	return r.Handler()
}

func IndexHandler(ctx *gin.Context) {
	// return a plaintext
	ctx.String(http.StatusOK, "SyftBox GO")
}

func HealthHandler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}
