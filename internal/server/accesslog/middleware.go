package accesslog

import (
	"github.com/gin-gonic/gin"
)

type AccessLogMiddleware struct {
	logger *AccessLogger
}

func NewMiddleware(logger *AccessLogger) *AccessLogMiddleware {
	return &AccessLogMiddleware{
		logger: logger,
	}
}

func (m *AccessLogMiddleware) Handler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set("access_logger", m.logger)
		ctx.Next()
	}
}

func GetAccessLogger(ctx *gin.Context) *AccessLogger {
	if logger, exists := ctx.Get("access_logger"); exists {
		if al, ok := logger.(*AccessLogger); ok {
			return al
		}
	}
	return nil
}