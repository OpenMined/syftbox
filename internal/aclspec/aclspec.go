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
func Exists(path string) bool {
	aclPath := AsAclPath(path)
	stat, err := os.Stat(aclPath)
	if os.IsNotExist(err) {
		return false
	}
	return stat.Size() > 0
}
