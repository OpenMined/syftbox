package sync

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
)

var (
	rejectedExt = ".syftrejected"
	conflictExt = ".syftconflict"
)

func MarkRejected(localPath string) error {
	// rename file from <path>/file.whatever to <path>/file.syftrejected.whatever
	ext := filepath.Ext(localPath)
	newPath := strings.Replace(localPath, ext, rejectedExt+ext, 1)
	return os.Rename(localPath, newPath)
}

func MarkConflicted(localPath string) error {
	// rename file from <path>/file.whatever to <path>/file.syftconflict.whatever
	ext := filepath.Ext(localPath)
	newPath := strings.Replace(localPath, ext, conflictExt+ext, 1)
	return os.Rename(localPath, newPath)
}

// WriteFile writes the body to the file at path and returns the md5 hash of the body
func WriteFile(path string, body []byte) (string, error) {
	if err := utils.EnsureParent(path); err != nil {
		return "", fmt.Errorf("ensure parent error: %w", err)
	}

	hasher := md5.New()

	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create file error: %w", err)
	}
	defer file.Close()

	writer := io.MultiWriter(file, hasher)

	if _, err := writer.Write(body); err != nil {
		return "", fmt.Errorf("write error: %w", err)
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
