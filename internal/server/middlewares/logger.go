package middlewares

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	slogGin "github.com/samber/slog-gin"
)

func Logger() gin.HandlerFunc {
	httpLogger := slog.Default().WithGroup("http")

	paths := []string{
		"/favicon.ico",
	}

	if gin.Mode() != gin.ReleaseMode {
		// disable logging for datasite view endpoint in dev mode
		// kinda pollutes the logs
		paths = append(paths, "/api/v1/datasite/view")
	}

	return slogGin.NewWithConfig(httpLogger, slogGin.Config{
		DefaultLevel:      slog.LevelInfo,
		ClientErrorLevel:  slog.LevelWarn,
		ServerErrorLevel:  slog.LevelError,
		WithRequestID:     true,
		WithRequestHeader: true,
		WithTraceID:       true,
		WithSpanID:        true,
		Filters: []slogGin.Filter{
			slogGin.IgnorePath(paths...),
		},
	})
}
