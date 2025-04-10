package utils

import (
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// FileHash calculates the MD5 hash of a file
func FileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	// Ensure the destination directory exists
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// CompareAndCopyFiles compares files between source and target directories and copies them if they differ.
// Returns true if any files were updated, false otherwise, and the number of files processed.
func CompareAndCopyFiles(sourceDir, targetDir string) (bool, int, error) {
	updated := false
	fileCount := 0

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Determine target file path
		targetPath := filepath.Join(targetDir, relPath)

		// Compare file hashes to determine if we need to copy
		needsCopy := true
		fileCount++

		// Check if target file exists
		if _, err := os.Stat(targetPath); err == nil {
			// Both files exist, compare their hashes
			oldHash, err := FileHash(targetPath)
			if err != nil {
				log.Printf("Warning: Failed to hash source file %s: %v", targetPath, err)
			} else {
				newHash, err := FileHash(path)
				if err != nil {
					log.Printf("Warning: Failed to hash target file %s: %v", path, err)
				} else if oldHash == newHash {
					// Files are identical, no need to copy
					needsCopy = false
				}
			}
		}

		if needsCopy {
			// Files are different or target doesn't exist, copy the file
			if err := CopyFile(path, targetPath); err != nil {
				return fmt.Errorf("failed to copy %s to %s: %v", path, targetPath, err)
			}
			updated = true
			fmt.Printf("Updated file: %s\n", relPath)
		}

		return nil
	})

	return updated, fileCount, err
}
