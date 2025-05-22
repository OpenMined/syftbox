package middlewares

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// server cors config
var corsConfig = cors.Config{
	AllowOrigins:     []string{"*"},
	AllowHeaders:     []string{"*"},
	AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
	AllowCredentials: false,
	AllowWebSockets:  true,
}

func CORS() gin.HandlerFunc {
	return cors.New(corsConfig)
}
