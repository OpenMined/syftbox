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

func EnsureParent(file string) error {
	dir := filepath.Dir(file)
	return os.MkdirAll(dir, 0755)
}

func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
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
