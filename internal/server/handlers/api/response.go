package api

import "github.com/gin-gonic/gin"

func AbortWithError(ctx *gin.Context, status int, code string, err error) {
	ctx.Abort()
	ctx.Error(err)
	ctx.PureJSON(status, SyftAPIError{
		Code:    code,
		Message: err.Error(),
	})
}
