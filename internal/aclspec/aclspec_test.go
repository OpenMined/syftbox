package aclspec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsACLFile(t *testing.T) {
	// Test the core ACL file detection logic
	// This is critical for the system to recognize permission files correctly
	testCases := []struct {
		path     string
		expected bool
		desc     string
	}{
		{
			path:     "syft.pub.yaml",
			expected: true,
			desc:     "Direct ACL filename should be detected",
		},
		{
			path:     "/path/to/syft.pub.yaml",
			expected: true,
			desc:     "ACL file with full path should be detected",
		},
		{
			path:     "folder/subfolder/syft.pub.yaml",
			expected: true,
			desc:     "ACL file in nested path should be detected",
		},
		{
			path:     "not_an_acl_file.yaml",
			expected: false,
			desc:     "Regular YAML file should not be detected as ACL",
		},
		{
			path:     "syft.pub.yaml.backup",
			expected: false,
			desc:     "ACL filename with suffix should not be detected",
		},
		{
			path:     "prefix_syft.pub.yaml",
			expected: true,
			desc:     "ACL filename with prefix should be detected (suffix match)",
		},
		{
			path:     "",
			expected: false,
			desc:     "Empty path should not be detected as ACL",
		},
		{
			path:     "syft.pub.yml",
			expected: false,
			desc:     "Wrong extension (.yml vs .yaml) should not be detected",
		},
		{
			path:     "SYFT.PUB.YAML",
			expected: false,
			desc:     "Case-sensitive check - uppercase should not match",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := IsACLFile(tc.path)
			assert.Equal(t, tc.expected, result, "Path: %s", tc.path)
		})
	}
}

func TestAsACLPath(t *testing.T) {
	// Test converting directory paths to ACL file paths
	// This ensures the system can correctly locate ACL files for directories
	testCases := []struct {
		input    string
		expected string
		desc     string
	}{
		{
			input:    filepath.Join("home", "user"),
			expected: filepath.Join("home", "user", "syft.pub.yaml"),
			desc:     "Directory path should get ACL filename appended",
		},
		{
			input:    filepath.Join("home", "user", "syft.pub.yaml"),
			expected: filepath.Join("home", "user", "syft.pub.yaml"),
			desc:     "ACL file path should remain unchanged",
		},
		{
			input:    "",
			expected: "syft.pub.yaml",
			desc:     "Empty path should result in just the ACL filename",
		},
		{
			input:    filepath.Join("relative", "path"),
			expected: filepath.Join("relative", "path", "syft.pub.yaml"),
			desc:     "Relative path should get ACL filename appended",
		},
		{
			input:    string(filepath.Separator),
			expected: string(filepath.Separator) + "syft.pub.yaml",
			desc:     "Root directory should get ACL filename appended",
		},
		{
			input:    filepath.Join("folder", "syft.pub.yaml"),
			expected: filepath.Join("folder", "syft.pub.yaml"),
			desc:     "Path already ending with ACL filename should be unchanged",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := AsACLPath(tc.input)
			assert.Equal(t, tc.expected, result, "Input: %s", tc.input)
		})
	}
}

func TestWithoutACLPath(t *testing.T) {
	// Test removing ACL filename from paths
	// This is used to get the directory path from an ACL file path
	testCases := []struct {
		input    string
		expected string
		desc     string
	}{
		{
			input:    filepath.Join("home", "user", "syft.pub.yaml"),
			expected: filepath.Join("home", "user") + string(filepath.Separator),
			desc:     "ACL file path should have filename removed",
		},
		{
			input:    "syft.pub.yaml",
			expected: "",
			desc:     "Bare ACL filename should result in empty string",
		},
		{
			input:    filepath.Join("home", "user", "other.yaml"),
			expected: filepath.Join("home", "user", "other.yaml"),
			desc:     "Non-ACL file path should remain unchanged",
		},
		{
			input:    filepath.Join("home", "user") + string(filepath.Separator),
			expected: filepath.Join("home", "user") + string(filepath.Separator),
			desc:     "Directory path without ACL filename should remain unchanged",
		},
		{
			input:    "",
			expected: "",
			desc:     "Empty path should remain empty",
		},
		{
			input:    filepath.Join("folder", "subfolder", "syft.pub.yaml"),
			expected: filepath.Join("folder", "subfolder") + string(filepath.Separator),
			desc:     "Nested ACL file path should have filename removed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := WithoutACLPath(tc.input)
			assert.Equal(t, tc.expected, result, "Input: %s", tc.input)
		})
	}
}

func TestExists(t *testing.T) {
	// Test ACL file existence detection
	// This validates the system can correctly detect existing ACL files on disk
	
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	
	// Create a test ACL file with some content
	aclFilePath := filepath.Join(tempDir, AclFileName)
	err := os.WriteFile(aclFilePath, []byte("terminal: true\nrules: []"), 0644)
	require.NoError(t, err, "Should be able to create test ACL file")
	
	// Test that existing non-empty ACL file is detected
	assert.True(t, Exists(tempDir), "Should detect existing ACL file in directory")
	assert.True(t, Exists(aclFilePath), "Should detect existing ACL file by direct path")
	
	// Create an empty ACL file
	emptyAclDir := filepath.Join(tempDir, "empty")
	err = os.Mkdir(emptyAclDir, 0755)
	require.NoError(t, err, "Should be able to create test directory")
	
	emptyAclFile := filepath.Join(emptyAclDir, AclFileName)
	err = os.WriteFile(emptyAclFile, []byte{}, 0644)
	require.NoError(t, err, "Should be able to create empty ACL file")
	
	// Test that empty ACL file is not considered as existing
	assert.False(t, Exists(emptyAclDir), "Empty ACL file should not be considered as existing")
	
	// Test that non-existent path returns false
	nonExistentPath := filepath.Join(tempDir, "nonexistent")
	assert.False(t, Exists(nonExistentPath), "Non-existent path should return false")
	
	// Test that directory without ACL file returns false
	noAclDir := filepath.Join(tempDir, "no_acl")
	err = os.Mkdir(noAclDir, 0755)
	require.NoError(t, err, "Should be able to create test directory")
	
	assert.False(t, Exists(noAclDir), "Directory without ACL file should return false")
}

func TestExistsWithSymlinks(t *testing.T) {
	// Test that symlinks are rejected for security reasons
	// ACL files must be regular files, not symlinks
	
	tempDir := t.TempDir()
	
	// Create actual ACL file
	realAclDir := filepath.Join(tempDir, "real")
	err := os.Mkdir(realAclDir, 0755)
	require.NoError(t, err)
	
	realAclFile := filepath.Join(realAclDir, AclFileName)
	err = os.WriteFile(realAclFile, []byte("terminal: true"), 0644)
	require.NoError(t, err)
	
	// Verify real ACL file is detected
	assert.True(t, Exists(realAclDir), "Real ACL file should be detected")
	
	// Create symlink to the ACL file
	symlinkDir := filepath.Join(tempDir, "symlink")
	err = os.Mkdir(symlinkDir, 0755)
	require.NoError(t, err)
	
	symlinkAclFile := filepath.Join(symlinkDir, AclFileName)
	err = os.Symlink(realAclFile, symlinkAclFile)
	require.NoError(t, err)
	
	// Test that symlinked ACL file is REJECTED for security reasons
	assert.False(t, Exists(symlinkDir), "Symlinked ACL files should be rejected for security")
	
	// Test that LoadFromFile also rejects symlinks
	_, err = LoadFromFile(symlinkDir)
	assert.Error(t, err, "LoadFromFile should reject symlinked ACL files")
	assert.Contains(t, err.Error(), "symlinks are not allowed", "Error should mention symlink restriction")
	
	// Create broken symlink
	brokenDir := filepath.Join(tempDir, "broken")
	err = os.Mkdir(brokenDir, 0755)
	require.NoError(t, err)
	
	brokenSymlink := filepath.Join(brokenDir, AclFileName)
	err = os.Symlink("/nonexistent/file", brokenSymlink)
	require.NoError(t, err)
	
	// Test that broken symlink is also rejected
	assert.False(t, Exists(brokenDir), "Broken symlinks should be rejected")
	
	// Test that LoadFromFile also rejects broken symlinks
	_, err = LoadFromFile(brokenDir)
	assert.Error(t, err, "LoadFromFile should reject broken symlinks")
	assert.Contains(t, err.Error(), "symlinks are not allowed", "Error should mention symlink restriction")
}

func TestSymlinkSecurityComprehensive(t *testing.T) {
	// Comprehensive test to ensure symlinks are rejected at all entry points
	// This is critical for security - no ACL operation should follow symlinks
	
	tempDir := t.TempDir()
	
	// Create a legitimate ACL file
	realAclFile := filepath.Join(tempDir, "real.yaml")
	aclContent := `
terminal: true
rules:
  - pattern: "**"
    access:
      read: ["admin@example.com"]
`
	err := os.WriteFile(realAclFile, []byte(aclContent), 0644)
	require.NoError(t, err)
	
	// Create a directory with a symlinked ACL file
	symlinkDir := filepath.Join(tempDir, "symlinked")
	err = os.Mkdir(symlinkDir, 0755)
	require.NoError(t, err)
	
	symlinkAclFile := filepath.Join(symlinkDir, AclFileName)
	err = os.Symlink(realAclFile, symlinkAclFile)
	require.NoError(t, err)
	
	// Test all ACL operations reject symlinks
	
	// 1. Exists should return false for symlinked ACL
	assert.False(t, Exists(symlinkDir), "Exists() should reject symlinked ACL files")
	
	// 2. LoadFromFile should error for symlinked ACL
	_, err = LoadFromFile(symlinkDir)
	assert.Error(t, err, "LoadFromFile() should reject symlinked ACL files")
	assert.Contains(t, err.Error(), "symlinks are not allowed", "Error should mention symlink restriction")
	
	// 3. AsACLPath and IsACLFile should work normally (they don't check file existence)
	aclPath := AsACLPath(symlinkDir)
	assert.Equal(t, symlinkAclFile, aclPath, "AsACLPath should work normally")
	assert.True(t, IsACLFile(aclPath), "IsACLFile should work normally")
	
	// 4. Test that WithoutACLPath works normally
	dirPath := WithoutACLPath(aclPath)
	// Note: WithoutACLPath may add trailing separator, so we use filepath.Clean for comparison
	assert.Equal(t, symlinkDir, filepath.Clean(dirPath), "WithoutACLPath should work normally")
	
	// Verify that regular files still work
	regularDir := filepath.Join(tempDir, "regular")
	err = os.Mkdir(regularDir, 0755)
	require.NoError(t, err)
	
	regularAclFile := filepath.Join(regularDir, AclFileName)
	err = os.WriteFile(regularAclFile, []byte(aclContent), 0644)
	require.NoError(t, err)
	
	// Regular ACL files should work normally
	assert.True(t, Exists(regularDir), "Regular ACL files should work")
	
	_, err = LoadFromFile(regularDir)
	assert.NoError(t, err, "LoadFromFile should work with regular ACL files")
}

func TestConstants(t *testing.T) {
	// Test that the constants have expected values
	// This ensures the constants are correctly defined and haven't been accidentally changed
	assert.Equal(t, "syft.pub.yaml", AclFileName, "ACL filename constant should be correct")
	assert.Equal(t, "*", Everyone, "Everyone constant should be correct")
	assert.Equal(t, "**", AllFiles, "AllFiles pattern should be correct")
	assert.Equal(t, true, SetTerminal, "SetTerminal constant should be true")
	assert.Equal(t, false, UnsetTerminal, "UnsetTerminal constant should be false")
}

func TestPathEdgeCases(t *testing.T) {
	// Test edge cases in path manipulation functions
	// This ensures robust handling of unusual but valid path scenarios
	
	// Test with paths containing special characters
	specialPath := filepath.Join("home", "user with spaces", "syft.pub.yaml")
	assert.True(t, IsACLFile(specialPath), "Paths with spaces should be handled correctly")
	
	// Test with Unicode characters
	unicodePath := filepath.Join("home", "用户", "syft.pub.yaml")
	assert.True(t, IsACLFile(unicodePath), "Unicode paths should be handled correctly")
	
	// Test with multiple path separators - this behavior is platform-specific
	// On Unix: "/home//user///syft.pub.yaml" 
	// On Windows: "home\\\\user\\\\\\syft.pub.yaml"
	sep := string(filepath.Separator)
	multiSlashPath := "home" + sep + sep + "user" + sep + sep + sep + "syft.pub.yaml"
	// AsACLPath should preserve the input when it already ends with the ACL filename
	assert.Equal(t, multiSlashPath, AsACLPath(multiSlashPath), "Multiple separators should be preserved")
	
	// Test with platform-appropriate absolute paths
	if filepath.Separator == '\\' {
		// Windows-style paths
		windowsPath := "C:\\Users\\user\\syft.pub.yaml"
		assert.True(t, IsACLFile(windowsPath), "Windows-style paths should be detected")
	} else {
		// Unix-style paths
		unixPath := "/home/user/syft.pub.yaml"
		assert.True(t, IsACLFile(unixPath), "Unix-style paths should be detected")
	}
}