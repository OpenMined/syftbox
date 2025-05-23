package middlewares

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	slogGin "github.com/samber/slog-gin"
)

func Logger() gin.HandlerFunc {
	httpLogger := slog.Default().WithGroup("http")

	return slogGin.NewWithConfig(httpLogger, slogGin.Config{
		DefaultLevel:      slog.LevelInfo,
		ClientErrorLevel:  slog.LevelWarn,
		ServerErrorLevel:  slog.LevelError,
		WithRequestID:     true,
		WithRequestHeader: true,
		WithTraceID:       true,
		WithSpanID:        true,
	})
}
