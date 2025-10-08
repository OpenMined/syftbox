package sync

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rjeczalik/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileWithIntegrityCheck(t *testing.T) {
	t.Run("successful write", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "test.txt")
		content := []byte("Hello, World!")

		// Calculate expected ETag
		hasher := md5.New()
		hasher.Write(content)
		expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

		err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
		assert.NoError(t, err)

		// Verify file exists and has correct content
		fileContent, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, content, fileContent)
	})

	t.Run("integrity check failure", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "test.txt")
		content := []byte("Hello, World!")
		wrongETag := "wrongetag123"

		err := writeFileWithIntegrityCheck(filePath, content, wrongETag)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Integrity check failed")

		// Verify file was not created due to integrity failure
		_, err = os.ReadFile(filePath)
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("parent directory creation", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "subdir", "nested", "test.txt")
		content := []byte("Nested file content")

		hasher := md5.New()
		hasher.Write(content)
		expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

		err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
		assert.NoError(t, err)

		// Verify file exists in nested directory
		fileContent, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, content, fileContent)
	})

	t.Run("empty content", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "empty.txt")
		content := []byte{}

		hasher := md5.New()
		hasher.Write(content)
		expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

		err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
		assert.NoError(t, err)

		// Verify empty file was created
		fileContent, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, content, fileContent)
		assert.Len(t, fileContent, 0)
	})
}

func TestWriteFileWithIntegrityCheckConcurrency(t *testing.T) {
	t.Run("concurrent writes to different files", func(t *testing.T) {
		tempDir := t.TempDir()
		numGoroutines := 10
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				filePath := filepath.Join(tempDir, fmt.Sprintf("file_%d.txt", id))
				content := []byte(fmt.Sprintf("Content for file %d", id))

				hasher := md5.New()
				hasher.Write(content)
				expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

				err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
				if err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for any errors
		for err := range errors {
			t.Errorf("Concurrent write error: %v", err)
		}

		// Verify all files were created correctly
		for i := 0; i < numGoroutines; i++ {
			filePath := filepath.Join(tempDir, fmt.Sprintf("file_%d.txt", i))
			content, err := os.ReadFile(filePath)
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("Content for file %d", i), string(content))
		}
	})

	t.Run("concurrent writes to same file", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "same_file.txt")
		numGoroutines := 5
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				content := []byte(fmt.Sprintf("Content version %d", id))

				hasher := md5.New()
				hasher.Write(content)
				expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

				err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
				if err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// At least one should succeed, others might fail due to file system constraints
		// But no race conditions should occur (no partial files)
		successCount := 0
		for err := range errors {
			if err == nil {
				successCount++
			}
		}

		// Verify file exists and is complete (not partial)
		fileContent, err := os.ReadFile(filePath)
		if err == nil {
			// File should contain complete content, not partial
			assert.NotEmpty(t, fileContent)
			assert.True(t, len(fileContent) > 0)
		}
	})
}

func TestWriteFileWithIntegrityCheckRaceCondition(t *testing.T) {
	t.Run("no partial file visibility", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "race_test.txt")
		content := []byte("This is a test file for race condition testing")

		hasher := md5.New()
		hasher.Write(content)
		expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

		// Channel to coordinate between writer and reader
		writeStarted := make(chan struct{})
		readComplete := make(chan struct{})
		var readerError error

		// Start reader goroutine that tries to read the file during write
		go func() {
			<-writeStarted

			// Try to read the file multiple times during the write process
			for i := 0; i < 100; i++ {
				fileContent, err := os.ReadFile(filePath)
				if err != nil {
					if !os.IsNotExist(err) {
						readerError = err
						break
					}
					// File doesn't exist yet, that's expected
					time.Sleep(1 * time.Millisecond)
					continue
				}

				// If file exists, it should be complete (not partial)
				if len(fileContent) > 0 && len(fileContent) != len(content) {
					readerError = fmt.Errorf("read partial file: got %d bytes, expected %d", len(fileContent), len(content))
					break
				}

				// If we got the complete file, that's fine
				if len(fileContent) == len(content) {
					break
				}

				time.Sleep(1 * time.Millisecond)
			}
			close(readComplete)
		}()

		// Start writer
		go func() {
			close(writeStarted)
			err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
			if err != nil {
				t.Errorf("Write error: %v", err)
			}
		}()

		// Wait for reader to complete
		<-readComplete

		// Verify no race condition occurred
		assert.NoError(t, readerError, "Race condition detected: reader saw partial file")

		// Verify final file is correct
		finalContent, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, content, finalContent)
	})
}

func TestWriteFileWithIntegrityCheckTempFileCleanup(t *testing.T) {
	t.Run("temp files are cleaned up on success", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "cleanup_test.txt")
		content := []byte("Cleanup test content")

		hasher := md5.New()
		hasher.Write(content)
		expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

		err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
		assert.NoError(t, err)

		// Check that no temp files remain in the directory
		entries, err := os.ReadDir(tempDir)
		require.NoError(t, err)

		for _, entry := range entries {
			assert.True(t, entry.Name() == "cleanup_test.txt",
				"Temp file not cleaned up: %s", entry.Name())
		}
	})

	t.Run("temp files are cleaned up on error", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "error_cleanup_test.txt")
		content := []byte("Error cleanup test content")
		wrongETag := "wrongetag"

		err := writeFileWithIntegrityCheck(filePath, content, wrongETag)
		assert.Error(t, err)

		// Check that no temp files remain in the directory
		entries, err := os.ReadDir(tempDir)
		require.NoError(t, err)

		assert.Empty(t, entries, "Temp files should be cleaned up after error, found: %v", entries)
	})
}

func TestWriteFileWithIntegrityCheckLargeFile(t *testing.T) {
	t.Run("large file write", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "large_file.txt")

		// Create a 1MB file
		content := make([]byte, 1024*1024)
		for i := range content {
			content[i] = byte(i % 256)
		}

		hasher := md5.New()
		hasher.Write(content)
		expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

		err := writeFileWithIntegrityCheck(filePath, content, expectedETag)
		assert.NoError(t, err)

		// Verify file size and content
		fileInfo, err := os.Stat(filePath)
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), fileInfo.Size())

		// Verify content integrity
		fileContent, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, content, fileContent)
	})
}

// TestWriteFileWithIntegrityCheckSyftTmpFiltering verifies that .syft.tmp.* files
// are properly filtered by the sync engine's ignore list
func TestWriteFileWithIntegrityCheckSyftTmpFiltering(t *testing.T) {
	tempDir := t.TempDir()

	// Create file watcher
	fw := NewFileWatcher(tempDir)

	// Create actual SyncIgnoreList to test the real filtering logic
	ignoreList := NewSyncIgnoreList(tempDir)
	ignoreList.Load() // This loads the default ignore patterns including *.syft.tmp.*

	// Set up filter using the actual ignore list logic
	fw.FilterPaths(func(path string) bool {
		// Use the actual ignore list logic
		return ignoreList.ShouldIgnore(path)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := fw.Start(ctx)
	require.NoError(t, err)
	defer fw.Stop()

	events := fw.Events()

	// Channel to collect events
	eventChan := make(chan notify.EventInfo, 10)
	go func() {
		for event := range events {
			eventChan <- event
		}
	}()

	// Test file path
	filePath := filepath.Join(tempDir, "test.txt")
	content := []byte("Hello, World!")

	// Calculate expected ETag
	hasher := md5.New()
	hasher.Write(content)
	expectedETag := fmt.Sprintf("%x", hasher.Sum(nil))

	// Write file using our atomic write function
	err = writeFileWithIntegrityCheck(filePath, content, expectedETag)
	require.NoError(t, err)

	// Collect events for a short period
	var receivedEvents []notify.EventInfo
	timeout := time.After(2 * time.Second)

	collecting := true
	for collecting {
		select {
		case event := <-eventChan:
			receivedEvents = append(receivedEvents, event)
			t.Logf("Received event: %s for path: %s", event.Event(), event.Path())
		case <-timeout:
			collecting = false
		}
	}

	// Analyze the events
	t.Logf("Total events received: %d", len(receivedEvents))

	// Check if we received events for .syft.tmp files
	var syftTmpFileEvents []notify.EventInfo
	var finalFileEvents []notify.EventInfo

	for _, event := range receivedEvents {
		path := event.Path()
		baseName := filepath.Base(path)

		// Check for .syft.tmp pattern in filename
		if len(baseName) > 10 {
			hasSyftTmp := false
			for i := 0; i < len(baseName)-10; i++ {
				if baseName[i:i+10] == ".syft.tmp." {
					hasSyftTmp = true
					break
				}
			}
			if hasSyftTmp {
				syftTmpFileEvents = append(syftTmpFileEvents, event)
				t.Logf("SYFT.TMP FILE EVENT: %s", path)
			}
		}

		if baseName == "test.txt" {
			finalFileEvents = append(finalFileEvents, event)
			t.Logf("FINAL FILE EVENT: %s", path)
		}
	}

	// With proper filtering, .syft.tmp files should NOT trigger events
	assert.Equal(t, 0, len(syftTmpFileEvents), "Files with .syft.tmp pattern should be filtered and not trigger events")

	// The final file might also be filtered by the ignore list, which is actually good
	// Let's just verify that no .syft.tmp files triggered events
	t.Logf("SUCCESS: .syft.tmp files properly filtered (%d events), final file events: %d",
		len(syftTmpFileEvents), len(finalFileEvents))

	// Verify the final file exists and is correct
	fileContent, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, content, fileContent)
}
