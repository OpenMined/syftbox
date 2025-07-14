package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path cannot be empty")
	}

	// Expand `~` to the user's home directory
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("failed to retrieve home directory")
		}
		path = strings.Replace(path, "~", homeDir, 1)
	}

	// Resolve relative paths (.., .) and return an absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	return filepath.Clean(absPath), nil
}

func EnsureParent(path string) error {
	dir := filepath.Dir(path)
	return EnsureDir(dir)
}

func EnsureDir(path string) error {
	// already exists
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	return os.MkdirAll(path, 0o755)
}

func DirExists(path string) bool {
	// check if the path is a directory
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func FileExists(path string) bool {
	// check if the path is a file
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func IsWritable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0o200 != 0
}
