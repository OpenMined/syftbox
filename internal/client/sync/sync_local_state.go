package sync

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/openmined/syftbox/internal/client/workspace"
)

type SyncLocalState struct {
	rootDir   string
	lastState map[SyncPath]*FileMetadata // Stores the result of the last successful scan
	mu        sync.RWMutex
}

func NewSyncLocalState(rootDir string) *SyncLocalState {
	return &SyncLocalState{
		rootDir:   rootDir,
		lastState: make(map[SyncPath]*FileMetadata),
	}
}

func (s *SyncLocalState) Scan() (map[SyncPath]*FileMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	newState := make(map[SyncPath]*FileMetadata)

	err := filepath.WalkDir(s.rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk error %s: %w", path, walkErr)
		}

		if d == nil {
			return fmt.Errorf("walk error %s: nil directory entry", path)
		}

		if d.IsDir() {
			// Skip .data directory (internal sync state) and symlinks
			if d.Name() == ".data" {
				return filepath.SkipDir
			}
			return nil // Skip other directories
		}

		if d.Type()&fs.ModeSymlink != 0 {
			return nil // Skip symlinks
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("walk file info %s: %w", path, err)
		}

		// Get relative path
		relPath, err := filepath.Rel(s.rootDir, path)
		if err != nil {
			return fmt.Errorf("walk rel path: %s: %w", path, err)
		}
		syncRelPath := SyncPath(workspace.NormPath(relPath))

		// Etag
		var etag string
		prevMeta, exists := s.lastState[syncRelPath]

		if exists && prevMeta.Size == info.Size() && prevMeta.LastModified.Equal(info.ModTime()) {
			// File metadata matches cached state, reuse ETag
			etag = prevMeta.ETag
		} else {
			// File is new or modified, calculate ETag
			calculatedETag, err := calculateETag(path)
			if err != nil {
				return fmt.Errorf("failed to calculate ETag for %s: %w", path, err)
			}
			etag = calculatedETag
		}

		metadata := &FileMetadata{
			Path:         syncRelPath,
			Size:         info.Size(),
			LastModified: info.ModTime(),
			ETag:         etag,
			Version:      "",
		}

		newState[syncRelPath] = metadata
		return nil
	})

	if err != nil {
		// Error from WalkDir itself or a callback error that halted the scan.
		return nil, fmt.Errorf("local scan failed: %w", err)
	}

	// Update the cache for the next run
	s.lastState = newState

	return s.lastState, nil
}

// calculateETag opens a file, calculates its MD5 hash, and returns it as a hex string.
func calculateETag(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %q: %w", filePath, err)
	}
	defer file.Close()

	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("failed to copy file content for hashing %q: %w", filePath, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
