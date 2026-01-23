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

	absSub := filepath.Join(baseDir, "alice@example.com", "syft.sub.yaml")
	require.NoError(t, os.WriteFile(absSub, []byte("x"), 0o644))
	assert.True(t, ignore.ShouldIgnore(absSub), "syft.sub.yaml should be ignored by default")

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

func TestSyncIgnoreList_TempFilePatterns(t *testing.T) {
	baseDir := t.TempDir()
	ignore := NewSyncIgnoreList(baseDir)
	ignore.Load()

	// Test Rust-style download temp files: .filename.tmp-uuid
	rustTempPath := "alice@example.com/public/.syft.pub.yaml.tmp-8cd89f7b-1234-5678-abcd-123456789012"
	assert.True(t, ignore.ShouldIgnore(rustTempPath), "Rust download temp file should be ignored")

	// Test with leading dot pattern
	rustTempPath2 := "bob@example.com/app_data/.config.json.tmp-abcdef12-3456-7890"
	assert.True(t, ignore.ShouldIgnore(rustTempPath2), "Rust download temp with dot prefix should be ignored")

	// Test Go-style atomic write temp files: *.syft.tmp.*
	goTempPath := "alice@example.com/public/data.syft.tmp.123456"
	assert.True(t, ignore.ShouldIgnore(goTempPath), "Go atomic write temp file should be ignored")

	// Test that regular files are NOT ignored
	regularPath := "alice@example.com/public/data.txt"
	assert.False(t, ignore.ShouldIgnore(regularPath), "regular files should not be ignored")

	// Test that ACL files are NOT ignored
	aclPath := "alice@example.com/public/syft.pub.yaml"
	assert.False(t, ignore.ShouldIgnore(aclPath), "ACL files should not be ignored")
}

func TestIsTempFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{"Rust download temp", ".syft.pub.yaml.tmp-8cd89f7b-1234", true},
		{"Rust download temp 2", ".config.json.tmp-abcdef", true},
		{"Go atomic temp", "file.syft.tmp.123456", true},
		{"Regular file", "data.txt", false},
		{"ACL file", "syft.pub.yaml", false},
		{"Rejected file", "file.rejected.txt", false},
		{"Conflict file", "file.conflict.txt", false},
		{"File ending in tmp", "backup.tmp", false}, // Not our temp pattern
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsTempFile(tc.filename)
			assert.Equal(t, tc.expected, result, "IsTempFile(%q) should be %v", tc.filename, tc.expected)
		})
	}
}

func TestCleanupOrphanedTempFiles(t *testing.T) {
	baseDir := t.TempDir()

	// Create directory structure
	datasitesDir := filepath.Join(baseDir, "datasites")
	aliceDir := filepath.Join(datasitesDir, "alice@example.com", "public")
	bobDir := filepath.Join(datasitesDir, "bob@example.com", "app_data")
	require.NoError(t, os.MkdirAll(aliceDir, 0o755))
	require.NoError(t, os.MkdirAll(bobDir, 0o755))

	// Create temp files that should be cleaned up
	tempFile1 := filepath.Join(aliceDir, ".syft.pub.yaml.tmp-8cd89f7b-1234")
	tempFile2 := filepath.Join(bobDir, ".config.json.tmp-abcdef12")
	tempFile3 := filepath.Join(aliceDir, "data.syft.tmp.123456")
	require.NoError(t, os.WriteFile(tempFile1, []byte("temp1"), 0o644))
	require.NoError(t, os.WriteFile(tempFile2, []byte("temp2"), 0o644))
	require.NoError(t, os.WriteFile(tempFile3, []byte("temp3"), 0o644))

	// Create regular files that should NOT be cleaned up
	regularFile := filepath.Join(aliceDir, "data.txt")
	aclFile := filepath.Join(aliceDir, "syft.pub.yaml")
	require.NoError(t, os.WriteFile(regularFile, []byte("regular"), 0o644))
	require.NoError(t, os.WriteFile(aclFile, []byte("acl content"), 0o644))

	// Run cleanup
	cleaned, errs := CleanupOrphanedTempFiles(datasitesDir)

	// Verify results
	assert.Empty(t, errs, "cleanup should not have errors")
	assert.Equal(t, 3, cleaned, "should have cleaned up 3 temp files")

	// Verify temp files are gone
	assert.NoFileExists(t, tempFile1, "temp file 1 should be removed")
	assert.NoFileExists(t, tempFile2, "temp file 2 should be removed")
	assert.NoFileExists(t, tempFile3, "temp file 3 should be removed")

	// Verify regular files still exist
	assert.FileExists(t, regularFile, "regular file should still exist")
	assert.FileExists(t, aclFile, "ACL file should still exist")
}
