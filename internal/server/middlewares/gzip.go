package middlewares

import (
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

var (
	excludedPaths = []string{
		"/healthz",
		"/releases",
	}
	excludedExtensions = []string{
		".png", ".gif", ".jpeg", ".jpg", ".webp", ".ico",
		".zip", ".tar", ".gz", ".bz2", ".rar", ".7z",
		".woff", ".woff2", ".ttf", ".otf",
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	}
)

func GZIP() gin.HandlerFunc {
	return gzip.Gzip(
		gzip.BestSpeed,
		gzip.WithExcludedPaths(excludedPaths),
		gzip.WithExcludedExtensions(excludedExtensions),
	)
}
