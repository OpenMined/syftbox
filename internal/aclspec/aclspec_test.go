package aclspec

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsACLFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "exact match",
			path:     "syft.pub.yaml",
			expected: true,
		},
		{
			name:     "with directory",
			path:     "user/syft.pub.yaml",
			expected: true,
		},
		{
			name:     "not an ACL file",
			path:     "user/file.txt",
			expected: false,
		},
		{
			name:     "partial match",
			path:     "user/mysyft.pub.yaml",
			expected: false,
		},
		{
			name:     "case sensitive",
			path:     "user/SYFT.PUB.YAML",
			expected: false,
		},
	}

	// Add Windows-specific test cases
	if runtime.GOOS == "windows" {
		tests = append(tests, []struct {
			name     string
			path     string
			expected bool
		}{
			{
				name:     "Windows path with backslashes",
				path:     `C:\Users\test\syft.pub.yaml`,
				expected: true,
			},
			{
				name:     "Windows path with forward slashes",
				path:     "C:/Users/test/syft.pub.yaml",
				expected: true,
			},
			{
				name:     "Windows UNC path",
				path:     `\\server\share\syft.pub.yaml`,
				expected: true,
			},
		}...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsACLFile(tt.path)
			if result != tt.expected {
				t.Errorf("IsACLFile(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestAsACLPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "directory path",
			path:     "user",
			expected: filepath.Join("user", FileName),
		},
		{
			name:     "already an ACL file",
			path:     "user/syft.pub.yaml",
			expected: "user/syft.pub.yaml",
		},
		{
			name:     "nested directory",
			path:     "user/data",
			expected: filepath.Join("user", "data", FileName),
		},
	}

	// Add Windows-specific test cases
	if runtime.GOOS == "windows" {
		tests = append(tests, []struct {
			name     string
			path     string
			expected string
		}{
			{
				name:     "Windows directory path",
				path:     `C:\Users\test`,
				expected: filepath.Join(`C:\Users\test`, FileName),
			},
			{
				name:     "Windows path already ACL file",
				path:     `C:\Users\test\syft.pub.yaml`,
				expected: `C:\Users\test\syft.pub.yaml`,
			},
		}...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AsACLPath(tt.path)
			if result != tt.expected {
				t.Errorf("AsACLPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestWithoutACLPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "path with ACL file",
			path:     "user/syft.pub.yaml",
			expected: "user/",
		},
		{
			name:     "path without ACL file",
			path:     "user/data",
			expected: "user/data",
		},
		{
			name:     "just ACL file",
			path:     "syft.pub.yaml",
			expected: "",
		},
	}

	// Add Windows-specific test cases
	if runtime.GOOS == "windows" {
		tests = append(tests, []struct {
			name     string
			path     string
			expected string
		}{
			{
				name:     "Windows path with ACL file",
				path:     `C:\Users\test\syft.pub.yaml`,
				expected: `C:\Users\test\`,
			},
			{
				name:     "Windows path without ACL file",
				path:     `C:\Users\test\data`,
				expected: `C:\Users\test\data`,
			},
		}...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WithoutACLPath(tt.path)
			if result != tt.expected {
				t.Errorf("WithoutACLPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}
