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
	// Extract the base name from the path
	base := filepath.Base(path)
	return base == AclFileName
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
func Exists(path string) bool {
	aclPath := AsACLPath(path)
	stat, err := os.Stat(aclPath)
	if os.IsNotExist(err) {
		return false
	}
	return stat.Size() > 0
}
