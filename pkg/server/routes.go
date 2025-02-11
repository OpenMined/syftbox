package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func SetupRoutes() http.Handler {
	r := gin.Default()
	r.Use(gin.Recovery())
	r.Use(cors.Default())

	r.GET("/", IndexHandler)
	{
		r.GET("/health", HealthHandler)
	}
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
