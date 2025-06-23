package blob

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsReservedPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		// Reserved paths that should be blocked
		{
			name:     "Direct api path",
			path:     "alice@example.com/api/something",
			expected: true,
		},
		{
			name:     "API path in public directory",
			path:     "alice@example.com/public/api/endpoint",
			expected: true,
		},
		{
			name:     "API path with subdirectory",
			path:     "alice@example.com/public/api/v1/users",
			expected: true,
		},
		{
			name:     "Well-known path",
			path:     "alice@example.com/public/.well-known/acme-challenge",
			expected: true,
		},
		{
			name:     "Internal path",
			path:     "alice@example.com/public/_internal/config",
			expected: true,
		},
		{
			name:     "Case insensitive API",
			path:     "alice@example.com/public/API/endpoint",
			expected: true,
		},
		{
			name:     "API in nested directory",
			path:     "alice@example.com/public/nested/api/test",
			expected: true,
		},
		
		// Valid paths that should be allowed
		{
			name:     "Normal file",
			path:     "alice@example.com/public/index.html",
			expected: false,
		},
		{
			name:     "File with api in name",
			path:     "alice@example.com/public/myapi.txt",
			expected: false,
		},
		{
			name:     "Directory with api prefix",
			path:     "alice@example.com/public/apitest/file.txt",
			expected: false,
		},
		{
			name:     "File named api.txt",
			path:     "alice@example.com/public/api.txt",
			expected: false,
		},
		{
			name:     "Valid nested path",
			path:     "alice@example.com/public/docs/readme.md",
			expected: false,
		},
		{
			name:     "Path with leading slash",
			path:     "/alice@example.com/public/file.txt",
			expected: false,
		},
		{
			name:     "Short path",
			path:     "alice@example.com",
			expected: false,
		},
		{
			name:     "Root path",
			path:     "/",
			expected: false,
		},
		{
			name:     "Empty path",
			path:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsReservedPath(tt.path)
			assert.Equal(t, tt.expected, result, "Path: %s", tt.path)
		})
	}
}