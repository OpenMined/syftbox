package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncIgnoreList_DefaultAndCustomRules(t *testing.T) {
	baseDir := t.TempDir()
	ignore := NewSyncIgnoreList(baseDir)

	// Default ignores should work even without a syftignore file.
	ignore.Load()

	absLog := filepath.Join(baseDir, "alice@example.com", "public", "debug.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(absLog), 0o755))
	require.NoError(t, os.WriteFile(absLog, []byte("x"), 0o644))
	assert.True(t, ignore.ShouldIgnore(absLog), "default *.log should ignore absolute paths")
	assert.True(t, ignore.ShouldIgnore("alice@example.com/public/debug.log"), "default *.log should ignore relative paths")

	absRequest := filepath.Join(baseDir, "alice@example.com", "app_data", "rpc", "a.request")
	require.NoError(t, os.MkdirAll(filepath.Dir(absRequest), 0o755))
	require.NoError(t, os.WriteFile(absRequest, []byte("x"), 0o644))
	assert.False(t, ignore.ShouldIgnore(absRequest), ".request files should not be ignored by default")

	// Custom syftignore appended after defaults.
	custom := []byte(`
# comment
**/*.request
private/**
`)
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "syftignore"), custom, 0o644))
	ignore.Load()

	assert.True(t, ignore.ShouldIgnore(absRequest), "custom **/*.request should now ignore")
	assert.True(t, ignore.ShouldIgnore("alice@example.com/private/file.txt"), "custom private/** should ignore")
	assert.False(t, ignore.ShouldIgnore("alice@example.com/public/file.txt"), "unmatched paths not ignored")
}

func TestSyncIgnoreList_AbsoluteOutsideBaseDir_NotIgnored(t *testing.T) {
	baseDir := t.TempDir()
	ignore := NewSyncIgnoreList(baseDir)
	ignore.Load()

	outside := filepath.Join(t.TempDir(), "other.txt")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))
	assert.False(t, ignore.ShouldIgnore(outside), "files outside baseDir should not be ignored")
}

