package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormPath(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty-is-local-dir", "", "."},
		{"unix-relative", "./path/to/test/path", "path/to/test/path"},
		{"unix-absolute", "/var/lib/check/path", "var/lib/check/path"},
		{"windows-relative", "\\SyftBox\\user@example.com\\test.txt", "SyftBox/user@example.com/test.txt"},
		{"windows-absolute", "C:\\windows\\system32\\test.txt", "C:/windows/system32/test.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expected, NormPath(c.input))
		})
	}
}

func TestWorkspaceSetup_CreatesLayoutAndDefaultACLs(t *testing.T) {
	root := t.TempDir()
	user := "alice@example.com"

	w, err := NewWorkspace(root, user)
	require.NoError(t, err)

	require.NoError(t, w.Setup())
	t.Cleanup(func() { _ = w.Unlock() })

	assert.DirExists(t, w.AppsDir)
	assert.DirExists(t, w.MetadataDir)
	assert.DirExists(t, w.DatasitesDir)
	assert.DirExists(t, w.UserPublicDir)

	assert.FileExists(t, filepath.Join(w.UserDir, "syft.pub.yaml"))
	assert.FileExists(t, filepath.Join(w.UserPublicDir, "syft.pub.yaml"))
}

func TestWorkspaceLocking_SingleInstance(t *testing.T) {
	root := t.TempDir()
	user := "alice@example.com"

	w1, err := NewWorkspace(root, user)
	require.NoError(t, err)
	w2, err := NewWorkspace(root, user)
	require.NoError(t, err)

	require.NoError(t, w1.Lock())

	err = w2.Lock()
	require.ErrorIs(t, err, ErrWorkspaceLocked)

	lockPath := filepath.Join(root, ".data", "syftbox.lock")
	assert.FileExists(t, lockPath)

	require.NoError(t, w1.Unlock())
	_, statErr := os.Stat(lockPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	require.NoError(t, w2.Lock())
	t.Cleanup(func() { _ = w2.Unlock() })
}
