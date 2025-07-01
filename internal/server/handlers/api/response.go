package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func AbortWithError(ctx *gin.Context, status int, code string, err error) {
	ctx.Abort()
	ctx.Error(err)
	ctx.PureJSON(status, SyftAPIError{
		Code:    code,
		Message: err.Error(),
	})
}

func Serve404HTML(ctx *gin.Context) {
	ctx.Abort()
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(http.StatusNotFound, "<h1>404 Not Found</h1><p>The requested resource <b><code>%s</code></b> was not found on this server.</p>", ctx.Request.URL.Path)
}

func Serve403HTML(ctx *gin.Context) {
	ctx.Abort()
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(http.StatusForbidden, "<h1>403 Forbidden</h1><p>You do not have permissions to access <b><code>%s</code></b> on this server.</p>", ctx.Request.URL.Path)
}

func Serve500HTML(ctx *gin.Context, err error) {
	ctx.Abort()
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(http.StatusInternalServerError, "<h1>500 Internal Server Error</h1><p>The server encountered an internal error. Please try again later.</p><code>%s</code>", err.Error())
}

func ServeErrorHTML(ctx *gin.Context, status int, title string, message string) {
	ctx.Abort()
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(status, "<h1>%s</h1><p>%s</p>", title, message)
}
