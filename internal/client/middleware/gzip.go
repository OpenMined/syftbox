package middleware

import (
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

var (
	excludedPaths = []string{
		"/health",
	}
	excludedExtensions = []string{
		".png", ".gif", ".jpeg", ".jpg", ".mp4", ".mov", ".mp3", ".wav", ".pdf", ".zip", ".tar.gz",
	}
)

// CompressionConfig contains configuration for the compression middleware
type CompressionConfig struct {
	// Level is the compression level (1-9)
	Level int
	// ExcludedPaths are paths that should not be compressed
	ExcludedPaths []string
	// ExcludedExtensions are file extensions that should not be compressed
	ExcludedExtensions []string
}

func Gzip() gin.HandlerFunc {
	return gzip.Gzip(
		gzip.DefaultCompression,
		gzip.WithExcludedPaths(excludedPaths),
		gzip.WithExcludedExtensions(excludedExtensions),
	)
}

func GzipWithConfig(config CompressionConfig) gin.HandlerFunc {
	return gzip.Gzip(
		config.Level,
		gzip.WithExcludedPaths(config.ExcludedPaths),
		gzip.WithExcludedExtensions(config.ExcludedExtensions),
	)
}
