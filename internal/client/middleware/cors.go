package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// control plane cors config
var corsConfig = cors.Config{
	AllowAllOrigins: true,
	AllowMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
	AllowHeaders: []string{
		"Origin",
		"Content-Length",
		"Content-Type",
		"Authorization",
	},
	AllowCredentials: true,
	MaxAge:           12 * time.Hour,
}

func CORS() gin.HandlerFunc {
	return cors.New(corsConfig)
}
