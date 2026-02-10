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

func TestListMarkedFiles(t *testing.T) {
	baseDir := t.TempDir()

	// Create directory structure
	aliceDir := filepath.Join(baseDir, "alice@example.com", "public")
	bobDir := filepath.Join(baseDir, "bob@example.com", "shared")
	require.NoError(t, os.MkdirAll(aliceDir, 0o755))
	require.NoError(t, os.MkdirAll(bobDir, 0o755))

	// Create conflict files
	conflict1 := filepath.Join(aliceDir, "data.conflict.txt")
	conflict2 := filepath.Join(bobDir, "config.conflict.json")
	require.NoError(t, os.WriteFile(conflict1, []byte("conflict1"), 0o644))
	require.NoError(t, os.WriteFile(conflict2, []byte("conflict2"), 0o644))

	// Create rejected files
	rejected1 := filepath.Join(aliceDir, "secret.rejected.txt")
	rejected2 := filepath.Join(bobDir, "private.rejected.json")
	require.NoError(t, os.WriteFile(rejected1, []byte("rejected1"), 0o644))
	require.NoError(t, os.WriteFile(rejected2, []byte("rejected2"), 0o644))

	// Create a legacy marker file
	legacyRejected := filepath.Join(aliceDir, "old.syftrejected.txt")
	require.NoError(t, os.WriteFile(legacyRejected, []byte("legacy"), 0o644))

	// Create regular files that should NOT be listed
	regularFile := filepath.Join(aliceDir, "normal.txt")
	require.NoError(t, os.WriteFile(regularFile, []byte("regular"), 0o644))

	// Run ListMarkedFiles
	conflicts, rejected, err := ListMarkedFiles(baseDir)
	require.NoError(t, err)

	// Verify results
	assert.Len(t, conflicts, 2, "should find 2 conflict files")
	assert.Len(t, rejected, 3, "should find 3 rejected files (including legacy)")

	// Verify conflict file details
	conflictPaths := make(map[string]bool)
	for _, f := range conflicts {
		conflictPaths[f.Path] = true
		assert.Equal(t, "conflict", f.MarkerType)
		assert.NotEmpty(t, f.OriginalPath)
		assert.NotContains(t, f.OriginalPath, ".conflict")
	}
	assert.True(t, conflictPaths["alice@example.com/public/data.conflict.txt"])
	assert.True(t, conflictPaths["bob@example.com/shared/config.conflict.json"])

	// Verify rejected file details
	rejectedPaths := make(map[string]bool)
	for _, f := range rejected {
		rejectedPaths[f.Path] = true
		assert.Equal(t, "rejected", f.MarkerType)
	}
	assert.True(t, rejectedPaths["alice@example.com/public/secret.rejected.txt"])
	assert.True(t, rejectedPaths["bob@example.com/shared/private.rejected.json"])
	assert.True(t, rejectedPaths["alice@example.com/public/old.syftrejected.txt"])
}
