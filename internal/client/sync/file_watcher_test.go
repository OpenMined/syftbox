package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rjeczalik/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileWatcher(t *testing.T) {
	fw := NewFileWatcher("/test/path")

	assert.Equal(t, "/test/path", fw.watchDir)
	assert.Nil(t, fw.events)
	assert.Nil(t, fw.rawEvents)
	assert.NotNil(t, fw.ignore)
	assert.NotNil(t, fw.done)
	assert.Empty(t, fw.ignore)
}

func TestFileWatcherBasic(t *testing.T) {
	// Create a temp directory to watch
	tempDir := t.TempDir()
	defer os.RemoveAll(tempDir)

	// macos is funny =)
	// tmpdir lives in /var/folders but it's actually symlink to /private/var/folders
	tempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err, "failed to evaluate symlinks")

	// Create FileWatcher
	fw := NewFileWatcher(tempDir)

	// Start watching
	err = fw.Start(t.Context())
	require.NoError(t, err, "failed to start file watcher")
	defer fw.Stop()

	// Get the events channel
	events := fw.Events()

	// Write a file and expect an event
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("hello world"), 0644)
	require.NoError(t, err, "failed to write test.txt")

	// Wait for the event
	select {
	case event := <-events:
		assert.Equal(t, event.Event(), notify.Write)
		assert.Equal(t, event.Path(), testFile)
	case <-time.After(2 * time.Second):
		assert.FailNow(t, "Timeout waiting for file event")
	}
}

func TestFileWatcherIgnoreOnce(t *testing.T) {
	// Create a temp directory to watch
	tempDir := t.TempDir()

	// macos is funny =)
	// tmpdir lives in /var/folders but it's actually symlink to /private/var/folders
	tempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err, "failed to evaluate symlinks")

	// Create FileWatcher
	fw := NewFileWatcher(tempDir)

	// Start watching
	err = fw.Start(t.Context())
	require.NoError(t, err, "failed to start file watcher")
	defer fw.Stop()

	// Get the events channel
	events := fw.Events()

	// Write a file with ignore
	testFile := filepath.Join(tempDir, "ignored.txt")
	fw.IgnoreOnce(testFile)

	err = os.WriteFile(testFile, []byte("ignored content"), 0644)
	if err != nil {
		t.Fatalf("Failed to write ignored test file: %v", err)
	}

	// Expect no events within one second
	select {
	case event := <-events:
		assert.FailNow(t, "Expected no events, but got event for path", event.Path())
	case <-time.After(1 * time.Second):
		// This is expected - no events should come through
	}
}

func TestFileWatcherAutoCleanup(t *testing.T) {
	// Create a temp directory to watch
	tempDir := t.TempDir()

	// macos is funny =)
	// tmpdir lives in /var/folders but it's actually symlink to /private/var/folders
	tempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err, "failed to evaluate symlinks")

	// Create FileWatcher with short cleanup interval for testing
	fw := NewFileWatcher(tempDir)
	fw.SetCleanupInterval(50 * time.Millisecond) // Very short for testing

	// Start watching
	err = fw.Start(t.Context())
	require.NoError(t, err, "failed to start file watcher")
	defer fw.Stop()

	// Add some paths to ignore with very short timeouts
	testPath1 := filepath.Join(tempDir, "test1.txt")
	testPath2 := filepath.Join(tempDir, "test2.txt")

	fw.IgnoreOnceWithTimeout(testPath1, 20*time.Millisecond)  // Will expire quickly
	fw.IgnoreOnceWithTimeout(testPath2, 200*time.Millisecond) // Will survive longer

	// Verify entries are initially in the ignore map
	fw.ignoreMu.RLock()
	initialCount := len(fw.ignore)
	fw.ignoreMu.RUnlock()
	assert.Equal(t, 2, initialCount, "should have 2 entries in ignore map initially")

	// Wait for the first entry to expire and be cleaned up
	time.Sleep(100 * time.Millisecond) // Enough time for cleanup to run and remove expired entries

	// Check that expired entry was cleaned up
	fw.ignoreMu.RLock()
	afterCleanupCount := len(fw.ignore)
	_, path1Exists := fw.ignore[testPath1]
	_, path2Exists := fw.ignore[testPath2]
	fw.ignoreMu.RUnlock()

	assert.Equal(t, 1, afterCleanupCount, "should have 1 entry remaining after cleanup")
	assert.False(t, path1Exists, "path1 should have been cleaned up")
	assert.True(t, path2Exists, "path2 should still exist")

	// Wait for the second entry to expire and be cleaned up
	time.Sleep(200 * time.Millisecond)

	fw.ignoreMu.RLock()
	finalCount := len(fw.ignore)
	fw.ignoreMu.RUnlock()

	assert.Equal(t, 0, finalCount, "all entries should have been cleaned up")
}

func TestFileWatcher_StopProperlyShutdown(t *testing.T) {
	tempDir := t.TempDir()
	tempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)

	fw := NewFileWatcher(tempDir)
	fw.SetCleanupInterval(10 * time.Millisecond) // Fast cleanup for testing

	err = fw.Start(t.Context())
	require.NoError(t, err)

	// Add some ignore entries
	fw.IgnoreOnce(filepath.Join(tempDir, "test1.txt"))
	fw.IgnoreOnce(filepath.Join(tempDir, "test2.txt"))

	// Verify entries exist
	fw.ignoreMu.RLock()
	entriesBeforeStop := len(fw.ignore)
	fw.ignoreMu.RUnlock()
	assert.Equal(t, 2, entriesBeforeStop)

	// Stop should complete quickly without hanging
	done := make(chan struct{})
	go func() {
		fw.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Expected - Stop() completed
	case <-time.After(1 * time.Second):
		assert.Fail(t, "Stop() took too long, goroutines may not have shut down properly")
	}

	// Events channel should be closed
	select {
	case _, ok := <-fw.Events():
		assert.False(t, ok, "events channel should be closed after Stop()")
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "events channel should be closed and readable immediately")
	}
}

func TestFileWatcher_MultipleIgnoresSameFile(t *testing.T) {
	tempDir := t.TempDir()
	tempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)

	fw := NewFileWatcher(tempDir)
	err = fw.Start(t.Context())
	require.NoError(t, err)
	defer fw.Stop()

	events := fw.Events()
	testFile := filepath.Join(tempDir, "test.txt")

	// Add ignore twice for same file - second should overwrite first
	fw.IgnoreOnceWithTimeout(testFile, 1*time.Hour)         // Long timeout
	fw.IgnoreOnceWithTimeout(testFile, 50*time.Millisecond) // Short timeout overwrites

	// Write file - should be ignored initially
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	select {
	case event := <-events:
		assert.Fail(t, "expected no event due to ignore, but got", event.Path())
	case <-time.After(20 * time.Millisecond):
		// Expected
	}

	// Wait for the SHORT timeout to expire (not the hour-long one)
	time.Sleep(100 * time.Millisecond)

	// Should get event now since short timeout expired
	err = os.WriteFile(testFile, []byte("new content"), 0644)
	require.NoError(t, err)

	select {
	case event := <-events:
		assert.Equal(t, notify.Write, event.Event())
		assert.Equal(t, testFile, event.Path())
	case <-time.After(1 * time.Second):
		assert.Fail(t, "expected event after short ignore timeout expired")
	}
}

func TestFileWatcher_FilterPaths_NilCallback(t *testing.T) {
	tempDir := t.TempDir()
	tempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)

	fw := NewFileWatcher(tempDir)

	// Set filter to nil (no filtering)
	fw.FilterPaths(nil)

	err = fw.Start(t.Context())
	require.NoError(t, err)
	defer fw.Stop()

	events := fw.Events()

	// Create a file - should generate an event since no filter is active
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Should receive the event
	select {
	case event := <-events:
		assert.Equal(t, notify.Write, event.Event())
		assert.Equal(t, testFile, event.Path())
	case <-time.After(2 * time.Second):
		assert.Fail(t, "expected event but got timeout")
	}
}

func TestFileWatcher_FilterPaths_MultipleCriteria(t *testing.T) {
	tempDir := t.TempDir()
	tempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)

	fw := NewFileWatcher(tempDir)

	// Set up filter to ignore both .tmp files and files containing "ignore" in the name
	fw.FilterPaths(func(path string) bool {
		filename := filepath.Base(path)
		return filepath.Ext(path) == ".tmp" ||
			filepath.Ext(path) == ".log" ||
			filepath.Base(filename) == "ignore_me.txt" ||
			filepath.Base(filename) == "file.syft.tmp.123456"
	})

	err = fw.Start(t.Context())
	require.NoError(t, err)
	defer fw.Stop()

	events := fw.Events()

	// Create various files
	files := []string{
		"should_see.txt",  // Should generate event
		"temp.tmp",        // Should be filtered
		"debug.log",       // Should be filtered
		"ignore_me.txt",   // Should be filtered
		"file.syft.tmp.123456", // Should be filtered
		"normal_file.doc", // Should generate event
	}

	for _, filename := range files {
		fullPath := filepath.Join(tempDir, filename)
		err = os.WriteFile(fullPath, []byte("content"), 0644)
		require.NoError(t, err, "failed to write %s", filename)
	}

	// Collect events for a reasonable time period
	var receivedEvents []notify.EventInfo
	timeout := time.After(3 * time.Second)

	for {
		select {
		case event := <-events:
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			goto done
		}

		// If we've received 2 events (the expected non-filtered ones), we can stop early
		if len(receivedEvents) >= 2 {
			// Wait a bit more to ensure no filtered events come through
			select {
			case extraEvent := <-events:
				receivedEvents = append(receivedEvents, extraEvent)
			case <-time.After(200 * time.Millisecond):
				goto done
			}
		}
	}

done:
	// Should have received exactly 2 events (for files that shouldn't be filtered)
	expectedEvents := []string{"should_see.txt", "normal_file.doc"}
	require.Len(t, receivedEvents, 2, "expected exactly 2 events")

	receivedPaths := make([]string, len(receivedEvents))
	for i, event := range receivedEvents {
		receivedPaths[i] = filepath.Base(event.Path())
		assert.Equal(t, notify.Write, event.Event())
	}

	// Check that we got events for the right files (order might vary)
	for _, expectedFile := range expectedEvents {
		found := false
		for _, receivedPath := range receivedPaths {
			if receivedPath == expectedFile {
				found = true
				break
			}
		}
		assert.True(t, found, "expected to receive event for %s", expectedFile)
	}
}
