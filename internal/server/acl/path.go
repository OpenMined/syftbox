package acl

import (
	"path/filepath"
	"strings"
)

// The ACL system follows the Unix file system hierarchy.
const ACLPathSep = "/"

// ACLNormPath normalizes a file system path for use in ACL operations by:
// 1. Converting all path separators to forward slashes
// 2. Cleaning the path (resolving . and ..)
// 3. Removing leading path separators
// This ensures consistent path handling across different operating systems
// and compatibility with glob pattern matching.
func ACLNormPath(path string) string {
	return strings.TrimLeft(filepath.ToSlash(filepath.Clean(path)), ACLPathSep)
}

// ACLPathSegments splits a file system path into its component segments.
// It first normalizes the path using ACLNormPath to ensure consistent handling
// across operating systems, then splits it into segments using the ACL path separator.
func ACLPathSegments(path string) []string {
	return strings.Split(ACLNormPath(path), ACLPathSep)
}

// ACLJoinPath joins multiple path segments into a single normalized path string.
// It uses the ACL path separator and ensures forward slashes are used consistently
// across different operating systems. Each part can be a sub-path, so the result
// is normalized using filepath.ToSlash to handle any internal path separators.
func ACLJoinPath(parts ...string) string {
	// a part can be a sub-path so better to just call filepath.ToSlash
	return filepath.ToSlash(strings.Join(parts, ACLPathSep))
}
