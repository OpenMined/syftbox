package sync

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkers_SetAndRemoveMarker_WithRotation(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(orig, []byte("v1"), 0o644))

	// First mark.
	marked1, err := SetMarker(orig, Conflict)
	require.NoError(t, err)
	assert.True(t, IsConflictPath(marked1))
	assert.False(t, utils.FileExists(orig))

	// Create a new original and mark again to force rotation of existing marked file.
	require.NoError(t, os.WriteFile(orig, []byte("v2"), 0o644))
	marked2, err := SetMarker(orig, Conflict)
	require.NoError(t, err)
	assert.True(t, IsConflictPath(marked2))

	// There should be a rotated prior marker.
	unmarked := GetUnmarkedPath(marked2)
	assert.Equal(t, orig, unmarked)
	assert.True(t, ConflictFileExists(unmarked))

	// Remove marker returns to unmarked path.
	unmarkedPath, err := RemoveMarker(marked2)
	require.NoError(t, err)
	assert.Equal(t, orig, unmarkedPath)
	assert.True(t, utils.FileExists(orig))

	// Ensure rotated marker still exists and has timestamp suffix.
	ext := filepath.Ext(orig)
	base := orig[:len(orig)-len(ext)]
	rotatedMatches, err := filepath.Glob(base + string(Conflict) + ".*" + ext)
	require.NoError(t, err)
	rotatedRe := regexp.MustCompile(regexp.QuoteMeta(string(Conflict)) + `\.\d{14}` + regexp.QuoteMeta(ext) + `$`)
	foundRotated := false
	for _, p := range rotatedMatches {
		if rotatedRe.MatchString(p) {
			foundRotated = true
		}
	}
	assert.True(t, foundRotated, "expected rotated conflict marker to exist")
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

func TestSetMarker_Rejected_DedupesWithoutRotation(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "file.h5ad")
	require.NoError(t, os.WriteFile(orig, []byte("a"), 0o644))

	first, err := SetMarker(orig, Rejected)
	require.NoError(t, err)
	assert.True(t, IsRejectedPath(first))
	assert.True(t, RejectedFileExists(orig))

	// Recreate original file (simulating app rewriting) and mark again.
	require.NoError(t, os.WriteFile(orig, []byte("b"), 0o644))
	second, err := SetMarker(orig, Rejected)
	require.NoError(t, err)

	// Should keep the original marker path and not create rotated copies.
	assert.Equal(t, first, second)

	// Original file should be removed.
	_, err = os.Stat(orig)
	assert.True(t, os.IsNotExist(err))

	matches, err := filepath.Glob(filepath.Join(dir, "file.rejected*"))
	require.NoError(t, err)
	assert.Len(t, matches, 1)
}
