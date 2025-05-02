package blob

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateKey(t *testing.T) {

	longValidPath := strings.Repeat("a/", 1024)
	longInvalidPath := strings.Repeat("a\\", 1024)

	tests := []struct {
		name string
		key  string
		want bool
	}{
		// invalid length
		{name: "empty-key", key: "", want: false},
		{name: "key-too-long", key: longValidPath, want: false},
		{name: "key-too-long", key: longInvalidPath, want: false},
		// valid cases
		{name: "valid-key", key: "valid-key", want: true},
		{name: "valid-key-with-slashes", key: "valid/key/with/slashes", want: true},
		{name: "valid-path-to-✅", key: "valid/path/to/✅", want: true},
		// invalid cases
		{name: "invalid-key", key: ".", want: false},
		{name: "invalid-key", key: "..", want: false},
		{name: "invalid-path-with-backslashes", key: "invalid\\path\\with\\backslashes", want: false},
		{name: "invalid-path-to-✅", key: "invalid\\path\\to\\✅", want: false},
		{name: "invalid-relative-path", key: "invalid/../file", want: false},
		{name: "invalid-relative-path", key: "invalid/file/..", want: false},
		{name: "invalid-relative-path", key: "invalid/file/some..txt", want: false},
		{name: "invalid-path-leading-slash", key: "/invalid/path/file", want: false},
		{name: "invalid-path-leading-slashes", key: "//invalid/path/file", want: false},
		{name: "invalid-path-leading-slashes", key: "///invalid/path/file/", want: false},
		{name: "invalid-path-leading-backslash", key: "\\invalid\\path\\file", want: false},
		// UTF-8 validity
		{name: "invalid-utf8-sequence", key: "test\xffstring", want: false}, // \xff is an invalid UTF-8 byte
	}

	for _, test := range tests {
		assert.Equal(t, test.want, ValidateKey(test.key), test.name)
	}
}
