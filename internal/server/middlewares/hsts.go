package middlewares

import (
	"github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"
)

func HSTS() gin.HandlerFunc {
	return secure.New(secure.Config{
		SSLRedirect:          true,
		IsDevelopment:        false,
		STSSeconds:           315360000,
		STSIncludeSubdomains: true,
		STSPreload:           true,
		FrameDeny:            true,
		ContentTypeNosniff:   true,
		BrowserXssFilter:     true,
		IENoOpen:             true,
		SSLProxyHeaders:      map[string]string{"X-Forwarded-Proto": "https"},
	})
}
