package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormPath(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty-is-local-dir", "", "."},
		{"unix-relative", "./path/to/test/path", "path/to/test/path"},
		{"unix-absolute", "/var/lib/check/path", "var/lib/check/path"},
		{"windows-relative", "\\SyftBox\\user@example.com\\test.txt", "SyftBox/user@example.com/test.txt"},
		{"windows-absolute", "C:\\windows\\system32\\test.txt", "C:/windows/system32/test.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expected, NormPath(c.input))
		})
	}
}
