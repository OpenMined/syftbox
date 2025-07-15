package workspace

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	PathSep = string(filepath.Separator)

	// regexDatasitePath matches datasite paths that start with a valid email address followed by a slash
	// Valid: "user@example.com/path", "test.user@domain.co.uk/file.txt"
	// Invalid: "notanemail/path", "user@domain", "user@domain.", "user@.domain", "user@domain/path" (no slash)
	regexDatasitePath = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+/`)
)

// IsOwner checks if the user is the owner of the path
// The underlying assumption here is that owner is the prefix of the path
func IsOwner(path string, user string) bool {
	path = CleanPath(path)
	return strings.HasPrefix(path, user)
}

// IsValidPath checks if the path is a valid datasite path
// i.e. must start with a datasite email followed by a slash
func IsValidPath(path string) bool {
	return regexDatasitePath.MatchString(path)
}

// CleanPath returns a path with leading and trailing slashes removed
func CleanPath(path string) string {
	return strings.TrimLeft(filepath.Clean(path), PathSep)
}
