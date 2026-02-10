package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncPriorityList_DefaultPatterns(t *testing.T) {
	baseDir := t.TempDir()
	priority := NewSyncPriorityList(baseDir)

	absReq := filepath.Join(baseDir, "alice@example.com", "app_data", "rpc", "x.request")
	require.NoError(t, os.MkdirAll(filepath.Dir(absReq), 0o755))
	require.NoError(t, os.WriteFile(absReq, []byte("x"), 0o644))
	assert.True(t, priority.ShouldPrioritize(absReq))

	absResp := filepath.Join(baseDir, "alice@example.com", "app_data", "rpc", "x.response")
	require.NoError(t, os.WriteFile(absResp, []byte("x"), 0o644))
	assert.True(t, priority.ShouldPrioritize(absResp))

	absACL := filepath.Join(baseDir, "alice@example.com", "public", "syft.pub.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(absACL), 0o755))
	require.NoError(t, os.WriteFile(absACL, []byte("terminal: false\nrules: []\n"), 0o644))
	assert.True(t, priority.ShouldPrioritize(absACL))

	absOther := filepath.Join(baseDir, "alice@example.com", "public", "note.txt")
	require.NoError(t, os.WriteFile(absOther, []byte("x"), 0o644))
	assert.False(t, priority.ShouldPrioritize(absOther))
}

func TestSyncPriorityList_OutsideBaseDir_NotPrioritized(t *testing.T) {
	baseDir := t.TempDir()
	priority := NewSyncPriorityList(baseDir)

	outside := filepath.Join(t.TempDir(), "x.request")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))
	// Paths outside baseDir should not be prioritized
	assert.False(t, priority.ShouldPrioritize(outside))
}
