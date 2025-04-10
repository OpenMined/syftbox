package datasite

import (
	"path/filepath"
	"strings"
)

var pathSep = string(filepath.Separator)

// IsOwner checks if the user is the owner of the path
// The underlying assumption here is that owner is the prefix of the path
func IsOwner(path string, user string) bool {
	return strings.HasPrefix(strings.TrimLeft(filepath.Clean(path), pathSep), user)
}
