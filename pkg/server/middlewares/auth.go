package middlewares

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		user, _ := ctx.GetQuery("user")

		if user == "" {
			ctx.PureJSON(http.StatusBadRequest, gin.H{
				"error": "query param 'user' is required",
			})
			ctx.Abort()
			return
		}

		ctx.Set("user", user)
		ctx.Next()
	}
}
