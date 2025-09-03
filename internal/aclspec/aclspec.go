package aclspec

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	FileName      = "syft.pub.yaml"
	TokenUser     = "USER"
	TokenEveryone = "*"
	AllFiles      = "**"
	Terminal      = true
	NotTerminal   = false
)

// IsACLFile checks if the path is an syft.pub.yaml file
func IsACLFile(path string) bool {
	// Extract the base name from the path
	base := filepath.Base(path)
	return base == FileName
}

// AsACLPath converts any path to exact acl file path
func AsACLPath(path string) string {
	if IsACLFile(path) {
		return path
	}
	return filepath.Join(path, FileName)
}

// WithoutACLPath truncates syft.pub.yaml from the path
func WithoutACLPath(path string) string {
	return strings.TrimSuffix(path, FileName)
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
