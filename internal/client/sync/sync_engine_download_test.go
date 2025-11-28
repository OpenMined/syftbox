package sync

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyLocalWindowsRenameRetry(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "src.txt")
	dst := filepath.Join(tempDir, "dst.txt")

	err := os.WriteFile(src, []byte("new"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(dst, []byte("old"), 0o644)
	require.NoError(t, err)

	originalRename := renameFile
	originalGOOS := runtimeGOOS
	renameCalls := 0

	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		if renameCalls == 1 {
			return fs.ErrExist
		}
		// Perform a real rename on retry to ensure the file is moved.
		return os.Rename(oldpath, newpath)
	}
	runtimeGOOS = "windows"
	t.Cleanup(func() {
		renameFile = originalRename
		runtimeGOOS = originalGOOS
	})

	err = copyLocal(src, dst)
	require.NoError(t, err)

	assert.Equal(t, 2, renameCalls, "expected a retry after fs.ErrExist")

	content, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), content, "destination should contain the newly copied data")
}

func TestCopyLocalRenameErrorCleanup(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "src.txt")
	dst := filepath.Join(tempDir, "dst.txt")

	err := os.WriteFile(src, []byte("new"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(dst, []byte("old"), 0o644)
	require.NoError(t, err)

	originalRename := renameFile
	originalGOOS := runtimeGOOS
	renameFile = func(oldpath, newpath string) error {
		return fs.ErrExist
	}
	runtimeGOOS = "linux"
	t.Cleanup(func() {
		renameFile = originalRename
		runtimeGOOS = originalGOOS
	})

	err = copyLocal(src, dst)
	require.Error(t, err, "rename errors on non-Windows should bubble up")

	content, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), content, "destination should remain unchanged on failure")

	// Ensure temp files from the failed copy were cleaned up.
	tmpFiles, err := filepath.Glob(filepath.Join(tempDir, "dst.txt.tmp.*"))
	require.NoError(t, err)
	assert.Empty(t, tmpFiles, "temporary files should be removed after failure")
}
