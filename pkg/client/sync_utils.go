package client

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yashgorana/syftbox-go/pkg/utils"
)

func (sm *SyncManager) ignorePath(path string) {
	sm.mu.Lock()
	sm.syncd[path] = true
	sm.mu.Unlock()
}

func (sm *SyncManager) shouldIgnorePath(path string) bool {
	if strings.Contains(path, "syftrejected") {
		return true
	}
	// ignore just pulled files and then pop them
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if _, ok := sm.syncd[path]; ok {
		delete(sm.syncd, path)
		return true
	}
	return false
}

func RejectFile(path string) error {
	// rename file from <path>/file.whatever to <path>/file.syftrejected.whatever
	ext := filepath.Ext(path)
	newPath := strings.Replace(path, ext, ".syftrejected"+ext, 1)
	return os.Rename(path, newPath)
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
