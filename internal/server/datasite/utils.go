package datasite

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
)

var (
	PathSep = string(filepath.Separator)

	// regexDatasitePath matches datasite paths that start with a valid email address followed by a slash
	// Valid: "user@example.com/path", "test.user@domain.co.uk/file.txt"
	// Invalid: "notanemail/path", "user@domain", "user@domain.", "user@.domain", "user@domain/path" (no slash)
	regexDatasitePath = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+/`)

	// regexBadPaths matches paths that contain ".." "~" or backslashes
	regexBadPaths = regexp.MustCompile(`\.\.|^~|\\`)
)

// GetOwner extracts the owner from the datasite path
func GetOwner(path string) string {
	// clean path
	path = CleanPath(path)

	// get owner
	parts := strings.Split(path, PathSep)
	if len(parts) == 0 {
		return ""
	}

	email := parts[0]
	if !IsValidDatasite(email) {
		return ""
	}

	return email
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
// i.e. must start with a datasite email followed by a slash
func IsValidPath(path string) bool {
	return regexDatasitePath.MatchString(path)
}

func IsValidRelPath(path string) bool {
	return !regexBadPaths.MatchString(path)
}

func IsValidDatasite(user string) bool {
	return utils.IsValidEmail(user)
}

func isValidVanityDomainPath(path string) bool {
	return !regexBadPaths.MatchString(path)
}

// isHexString checks if a string contains only hex characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
