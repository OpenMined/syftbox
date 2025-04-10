package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindProjectRoot finds the root directory of the project by looking for go.mod file
func FindProjectRoot() (string, error) {
	// Start with current directory
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Look for go.mod file to identify project root
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		// Move up one directory
		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			// We've reached the filesystem root without finding go.mod
			return "", fmt.Errorf("could not find project root (no go.mod file found)")
		}
		dir = parentDir
	}
}
