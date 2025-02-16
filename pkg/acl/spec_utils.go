package acl

import (
	"path/filepath"
	"strings"
)

// AsAclPath converts any path to exact acl file path
func AsAclPath(path string) string {
	if IsAclFile(path) {
		return path
	}
	return filepath.Join(filepath.Clean(path), AclFileName)
}

// IsAclFile checks if the path is an syft.pub.yaml file
func IsAclFile(path string) bool {
	return strings.HasSuffix(filepath.Clean(path), AclFileName)
}

// PathWithoutAclFileName truncates syft.pub.yaml from the path
func PathWithoutAclFileName(path string) string {
	return strings.TrimSuffix(filepath.Clean(path), AclFileName)
}

// IsOwner checks if the user is the owner of the path
// The underlying assumption here is that owner is the prefix of the path
func IsOwner(path string, user string) bool {
	return strings.HasPrefix(filepath.Clean(path), user)
}
