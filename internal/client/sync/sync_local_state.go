package sync

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/openmined/syftbox/internal/client/workspace"
)

type SyncLocalState struct {
	rootDir   string
	lastState map[string]*FileMetadata // Stores the result of the last successful scan
}

func NewSyncLocalState(rootDir string) *SyncLocalState {
	return &SyncLocalState{
		rootDir:   rootDir,
		lastState: make(map[string]*FileMetadata),
	}
}

func (s *SyncLocalState) Scan() (map[string]*FileMetadata, error) {
	newState := make(map[string]*FileMetadata)

	err := filepath.WalkDir(s.rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk error: %w", walkErr)
		}

		if d.IsDir() {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			slog.Warn("Failed to get file info", "path", path, "error", err)
			return nil // Skip this file
		}

		// Get relative path
		relPath, err := filepath.Rel(s.rootDir, path)
		if err != nil {
			return fmt.Errorf("walk rel path: %w", err)
		}
		relPath = workspace.NormPath(relPath)

		// Caching Logic
		var etag string
		prevMeta, exists := s.lastState[relPath]

		if exists && prevMeta.Size == info.Size() && prevMeta.LastModified.Equal(info.ModTime()) {
			// File metadata matches cached state, reuse ETag
			etag = prevMeta.ETag
		} else {
			// File is new or modified, calculate ETag
			calculatedETag, err := calculateETag(path)
			if err != nil {
				slog.Warn("Failed to calculate ETag", "file", path, "error", err)
				return nil // Skip this file if ETag calculation fails
			}
			etag = calculatedETag
		}

		metadata := &FileMetadata{
			Path:         relPath,
			Size:         info.Size(),
			LastModified: info.ModTime(),
			ETag:         etag,
			Version:      "",
		}

		newState[relPath] = metadata
		return nil
	})

	if err != nil {
		// Error from WalkDir itself or a callback error that halted the scan.
		return nil, fmt.Errorf("local scan failed: %w", err)
	}

	// Update the cache for the next run
	s.lastState = newState

	return newState, nil
}

// calculateETag opens a file, calculates its MD5 hash, and returns it as a hex string.
func calculateETag(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file '%s': %w", filePath, err)
	}
	defer file.Close()

	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("failed to copy file content for hashing '%s': %w", filePath, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
