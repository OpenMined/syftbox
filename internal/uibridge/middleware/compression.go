package middleware

import (
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
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

// DefaultCompressionConfig returns default compression configuration
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Level: 6, // Default compression level
		ExcludedPaths: []string{
			"/health",
		},
		ExcludedExtensions: []string{
			".png", ".gif", ".jpeg", ".jpg", ".mp4", ".mov", ".mp3", ".wav", ".pdf", ".zip", ".tar.gz",
		},
	}
}

// Compression creates a middleware for compressing response bodies
func Compression(config CompressionConfig) gin.HandlerFunc {
	// Create middleware with compression level
	return gzip.Gzip(config.Level, gzip.WithExcludedPaths(config.ExcludedPaths), gzip.WithExcludedExtensions(config.ExcludedExtensions))
}
