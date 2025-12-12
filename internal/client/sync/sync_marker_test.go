package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkers_SetAndRemoveMarker_WithRotation(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(orig, []byte("v1"), 0o644))

	// First mark.
	marked1, err := SetMarker(orig, Rejected)
	require.NoError(t, err)
	assert.True(t, IsRejectedPath(marked1))
	assert.False(t, utils.FileExists(orig))

	// Create a new original and mark again to force rotation of existing marked file.
	require.NoError(t, os.WriteFile(orig, []byte("v2"), 0o644))
	before := time.Now().Add(-time.Second)
	marked2, err := SetMarker(orig, Rejected)
	require.NoError(t, err)
	assert.True(t, IsRejectedPath(marked2))

	// There should be a rotated prior marker.
	unmarked := GetUnmarkedPath(marked2)
	assert.Equal(t, orig, unmarked)
	assert.True(t, RejectedFileExists(unmarked))

	// Remove marker returns to unmarked path.
	unmarkedPath, err := RemoveMarker(marked2)
	require.NoError(t, err)
	assert.Equal(t, orig, unmarkedPath)
	assert.True(t, utils.FileExists(orig))

	// Ensure rotated marker still exists and has timestamp suffix.
	ext := filepath.Ext(orig)
	base := orig[:len(orig)-len(ext)]
	rotatedMatches, err := filepath.Glob(base + string(Rejected) + ".*" + ext)
	require.NoError(t, err)
	foundRotated := false
	for _, p := range rotatedMatches {
		if info, statErr := os.Stat(p); statErr == nil && info.ModTime().After(before) {
			foundRotated = true
		}
	}
	assert.True(t, foundRotated, "expected rotated rejected marker to exist")
}

func TestMarkers_SetMarker_IdempotentOnAlreadyMarked(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(orig, []byte("v1"), 0o644))

	marked, err := SetMarker(orig, Conflict)
	require.NoError(t, err)
	require.True(t, IsConflictPath(marked))

	again, err := SetMarker(marked, Conflict)
	require.NoError(t, err)
	assert.Equal(t, marked, again, "already-marked file should be a no-op")
}

func TestMarkers_GetMarkersAndUnmarkedPath(t *testing.T) {
	path := "/tmp/a/b/file.conflict.20241212153045.txt"
	assert.True(t, IsMarkedPath(path))
	assert.Equal(t, []MarkerType{Conflict}, GetMarkers(path))
	assert.Equal(t, "/tmp/a/b/file.txt", GetUnmarkedPath(path))

	path2 := "/tmp/a/b/file.rejected.txt"
	assert.Equal(t, []MarkerType{Rejected}, GetMarkers(path2))
	assert.Equal(t, "/tmp/a/b/file.txt", GetUnmarkedPath(path2))
}

func TestMarkers_RemoveMarker_ErrorsWhenDestinationExists(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(orig, []byte("v1"), 0o644))
	marked, err := SetMarker(orig, Conflict)
	require.NoError(t, err)

	// Re-create original so destination exists.
	require.NoError(t, os.WriteFile(orig, []byte("v2"), 0o644))
	_, err = RemoveMarker(marked)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "destination file already exists")
}
