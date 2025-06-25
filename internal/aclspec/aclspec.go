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

// IsACLFile checks if the path is an syft.pub.yaml file
func IsACLFile(path string) bool {
	return strings.HasSuffix(path, AclFileName)
}

// AsACLPath converts any path to exact acl file path
func AsACLPath(path string) string {
	if IsACLFile(path) {
		return path
	}
	return filepath.Join(path, AclFileName)
}

// WithoutACLPath truncates syft.pub.yaml from the path
func WithoutACLPath(path string) string {
	return strings.TrimSuffix(path, AclFileName)
}

// Exists checks if the ACL file exists at the given path
// For security reasons, symlinks are not allowed as ACL files
func Exists(path string) bool {
	aclPath := AsACLPath(path)
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
