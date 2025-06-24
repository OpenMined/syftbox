package utils

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "empty path",
			input:     "",
			wantError: true,
		},
		{
			name:      "relative path",
			input:     "./test",
			wantError: false,
		},
		{
			name:      "absolute path",
			input:     "/tmp/test",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolvePath(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ResolvePath(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
			if !tt.wantError && result == "" {
				t.Errorf("ResolvePath(%q) returned empty string", tt.input)
			}
		})
	}
}

func TestWindowsPathHandling(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific tests on non-Windows platform")
	}

	// Test that Windows paths are handled correctly
	tests := []struct {
		name  string
		path  string
		isDir bool
	}{
		{
			name:  "Windows path with backslashes",
			path:  `C:\Windows\System32`,
			isDir: true,
		},
		{
			name:  "Windows path with forward slashes",
			path:  "C:/Windows/System32",
			isDir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just test that the path operations don't panic
			_ = filepath.Clean(tt.path)
			_ = filepath.Dir(tt.path)
			_ = filepath.Base(tt.path)
		})
	}
}