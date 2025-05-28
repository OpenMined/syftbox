package datasite

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
)

var (
	PathSep           = string(filepath.Separator)
	regexDatasitePath = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+/`)
)

// GetOwner returns the owner of the path
func GetOwner(path string) string {
	// clean path
	path = CleanPath(path)

	// get owner
	parts := strings.Split(path, PathSep)
	if len(parts) == 0 {
		return ""
	}

	return parts[0]
}

// IsOwner checks if the user is the owner of the path
// The underlying assumption here is that owner is the prefix of the path
func IsOwner(path string, user string) bool {
	path = CleanPath(path)
	return strings.HasPrefix(path, user)
}

// CleanPath returns a path with leading and trailing slashes removed
func CleanPath(path string) string {
	return strings.TrimLeft(filepath.Clean(path), PathSep)
}

// IsValidPath checks if the path is a valid datasite path
func IsValidPath(path string) bool {
	return regexDatasitePath.MatchString(path)
}

func IsValidDatasite(user string) bool {
	return utils.IsValidEmail(user)
}
