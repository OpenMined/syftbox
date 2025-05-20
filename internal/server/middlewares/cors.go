package middlewares

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// server cors config
var corsConfig = cors.Config{
	AllowAllOrigins: true,
	AllowMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
	AllowHeaders: []string{
		"Origin",
		"Content-Length",
		"Content-Type",
		"Authorization",
		"X-Syft-Msg-Type",
		"X-Syft-From",
		"X-Syft-To",
		"X-Syft-App",
		"X-Syft-AppEp",
		"X-Syft-Method",
		"X-Syft-Headers",
		"X-Syft-Status",
	},
	AllowCredentials: true,
	MaxAge:           12 * time.Hour,
}

func InitCORS() gin.HandlerFunc {
	return cors.New(corsConfig)
}
