package utils

import (
	"mime"
	"path/filepath"
	"strings"
)

func DetectContentType(key string) string {
	if isTextLike(key) {
		return "text/plain; charset=utf-8"
	} else if mimeType := mime.TypeByExtension(filepath.Ext(key)); mimeType != "" {
		return mimeType
	}
	return "application/octet-stream"
}

func isTextLike(key string) bool {
	return strings.HasSuffix(key, ".yaml") ||
		strings.HasSuffix(key, ".yml") ||
		strings.HasSuffix(key, ".toml") ||
		strings.HasSuffix(key, ".md")
}
