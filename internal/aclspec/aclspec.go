package aclspec

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	AclFileName   = "syft.pub.yaml"
	Everyone      = "*"
	AllFiles      = "**"
	SetTerminal   = true
	UnsetTerminal = false
)

// IsAclFile checks if the path is an syft.pub.yaml file
func IsAclFile(path string) bool {
	return strings.HasSuffix(path, AclFileName)
}

// AsAclPath converts any path to exact acl file path
func AsAclPath(path string) string {
	if IsAclFile(path) {
		return path
	}
	return filepath.Join(path, AclFileName)
}

// WithoutAclPath truncates syft.pub.yaml from the path
func WithoutAclPath(path string) string {
	return strings.TrimSuffix(path, AclFileName)
}

// Exists checks if the ACL file exists at the given path
// For security reasons, symlinks are not allowed as ACL files
func Exists(path string) bool {
	aclPath := AsAclPath(path)
	stat, err := os.Lstat(aclPath) // Use Lstat to not follow symlinks
	if os.IsNotExist(err) {
		return false
	}
	
	// Reject symlinks for security reasons
	if stat.Mode()&os.ModeSymlink != 0 {
		return false
	}
	
	return stat.Size() > 0
}
