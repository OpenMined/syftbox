package sync

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/openmined/syftbox/internal/utils"
)

// writeFileWithIntegrityCheck writes the body to the file at path and verifies the integrity of the file
// Uses atomic write with temporary file to prevent race conditions
func writeFileWithIntegrityCheck(tmpDirPath string, path string, body []byte, expectedETag string) error {
	if err := utils.EnsureParent(path); err != nil {
		return fmt.Errorf("Failed to ensure parent: %w", err)
	}

	// Create temporary file in same directory to ensure atomic operation
	// Uses pattern *.syft.tmp.* which is part of syftignore list to be ignored by sync engine
	err := utils.EnsureDir(tmpDirPath)
	if err != nil {
		return fmt.Errorf("Failed to ensure temp directory: %w", err)
	}
	tempFile, err := os.CreateTemp(tmpDirPath, filepath.Base(path)+".syft.tmp.*")
	if err != nil {
		return fmt.Errorf("Failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	success := false

	// Cleanup temp file only on failure
	defer func() {
		if !success {
			tempFile.Close()
			os.Remove(tempPath)
		}
	}()

	hasher := md5.New()
	writer := io.MultiWriter(tempFile, hasher)

	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("Failed to write to temp file: %w", err)
	}

	// Verify integrity before atomic move
	computedETag := fmt.Sprintf("%x", hasher.Sum(nil))
	if expectedETag != computedETag {
		return fmt.Errorf("Integrity check failed expected %q got %q", expectedETag, computedETag)
	}

	// Sync to disk before rename to ensure durability
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("Failed to sync temp file: %w", err)
	}

	// Close temp file before atomic move
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("Failed to close temp file: %w", err)
	}

	// Rename the temp file to the final path (atomic operation)
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("Failed to rename temp file to %s: %w", path, err)
	}

	success = true
	return nil
}
