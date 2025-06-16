package aclspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLegacyPermissionUnmarshalYAML(t *testing.T) {
	// Test unmarshaling legacy permission format from YAML
	// This validates backward compatibility with older permission file formats
	yamlContent := `
- path: "documents/*"
  user: "alice@research.org"
  permissions: ["read", "write"]
- path: "public/*.txt"
  user: "bob@university.edu"
  permissions: ["read"]
- path: "admin/*"
  user: "admin@company.com"
  permissions: ["read", "write", "admin"]
`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should successfully unmarshal legacy permission format")

	// Verify the correct number of rules were parsed
	assert.Len(t, legacyPerm.Rules, 3, "Should parse all three legacy rules")

	// Verify first rule (alice with read/write on documents)
	rule1 := legacyPerm.Rules[0]
	assert.Equal(t, "documents/*", rule1.Path, "First rule path should be correct")
	assert.Equal(t, "alice@research.org", rule1.User, "First rule user should be correct")
	assert.Len(t, rule1.Permissions, 2, "First rule should have 2 permissions")
	assert.Contains(t, rule1.Permissions, Read, "First rule should include read permission")
	assert.Contains(t, rule1.Permissions, Write, "First rule should include write permission")

	// Verify second rule (bob with read on public txt files)
	rule2 := legacyPerm.Rules[1]
	assert.Equal(t, "public/*.txt", rule2.Path, "Second rule path should be correct")
	assert.Equal(t, "bob@university.edu", rule2.User, "Second rule user should be correct")
	assert.Len(t, rule2.Permissions, 1, "Second rule should have 1 permission")
	assert.Contains(t, rule2.Permissions, Read, "Second rule should include read permission")

	// Verify third rule (admin with full permissions)
	rule3 := legacyPerm.Rules[2]
	assert.Equal(t, "admin/*", rule3.Path, "Third rule path should be correct")
	assert.Equal(t, "admin@company.com", rule3.User, "Third rule user should be correct")
	assert.Len(t, rule3.Permissions, 3, "Third rule should have 3 permissions")
	assert.Contains(t, rule3.Permissions, Read, "Third rule should include read permission")
	assert.Contains(t, rule3.Permissions, Write, "Third rule should include write permission")
	assert.Contains(t, rule3.Permissions, Execute, "Third rule should include admin permission")
}

func TestLegacyPermissionUnmarshalEmptyYAML(t *testing.T) {
	// Test unmarshaling empty legacy permission list
	// This ensures the system handles empty legacy files gracefully
	yamlContent := `[]`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should successfully unmarshal empty legacy permission list")

	assert.Empty(t, legacyPerm.Rules, "Empty YAML should result in empty rules list")
}

func TestLegacyPermissionUnmarshalInvalidYAML(t *testing.T) {
	// Test that invalid YAML structure is properly rejected
	// This ensures the system fails safely on malformed legacy files
	yamlContent := `"this_is_not_a_sequence"`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	assert.Error(t, err, "Should reject non-sequence YAML for legacy permissions")
	assert.Contains(t, err.Error(), "expected a sequence", "Error should indicate sequence was expected")
}

func TestLegacyPermissionUnmarshalPartialRule(t *testing.T) {
	// Test unmarshaling legacy rules with missing fields
	// This validates handling of incomplete legacy data
	yamlContent := `
- path: "test/*"
  user: "testuser@example.com"
  # permissions field intentionally missing
- path: "incomplete"
  # user and permissions fields missing
`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should handle legacy rules with missing fields")

	assert.Len(t, legacyPerm.Rules, 2, "Should parse both rules despite missing fields")

	// First rule should have path and user, but empty permissions
	rule1 := legacyPerm.Rules[0]
	assert.Equal(t, "test/*", rule1.Path)
	assert.Equal(t, "testuser@example.com", rule1.User)
	assert.Empty(t, rule1.Permissions, "Missing permissions should result in empty slice")

	// Second rule should have only path
	rule2 := legacyPerm.Rules[1]
	assert.Equal(t, "incomplete", rule2.Path)
	assert.Empty(t, rule2.User, "Missing user should result in empty string")
	assert.Empty(t, rule2.Permissions, "Missing permissions should result in empty slice")
}

func TestPermissionTypeConstants(t *testing.T) {
	// Test that permission type constants have expected values
	// This ensures the constants match the legacy format requirements
	assert.Equal(t, PermissionType("read"), Read, "Read permission constant should be correct")
	assert.Equal(t, PermissionType("create"), Create, "Create permission constant should be correct")
	assert.Equal(t, PermissionType("write"), Write, "Write permission constant should be correct")
	assert.Equal(t, PermissionType("admin"), Execute, "Execute/Admin permission constant should be correct")
}

func TestLegacyRulePermissionTypes(t *testing.T) {
	// Test that all permission types can be properly parsed
	// This validates the enum handling for different permission levels
	yamlContent := `
- path: "test/*"
  user: "testuser@example.com"
  permissions: ["read", "create", "write", "admin"]
`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should parse all permission types")

	rule := legacyPerm.Rules[0]
	assert.Len(t, rule.Permissions, 4, "Should have all 4 permission types")
	assert.Contains(t, rule.Permissions, Read, "Should contain read permission")
	assert.Contains(t, rule.Permissions, Create, "Should contain create permission")
	assert.Contains(t, rule.Permissions, Write, "Should contain write permission")
	assert.Contains(t, rule.Permissions, Execute, "Should contain admin/execute permission")
}

func TestLegacyRuleWithDuplicatePermissions(t *testing.T) {
	// Test handling of duplicate permissions in legacy format
	// This ensures duplicate permissions are handled gracefully
	yamlContent := `
- path: "test/*"
  user: "testuser@example.com"
  permissions: ["read", "read", "write", "read"]
`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should handle duplicate permissions")

	rule := legacyPerm.Rules[0]
	// YAML unmarshaling preserves duplicates in slice
	assert.Len(t, rule.Permissions, 4, "Should preserve all permission entries including duplicates")
	
	// Count actual unique permissions
	uniquePerms := make(map[PermissionType]bool)
	for _, perm := range rule.Permissions {
		uniquePerms[perm] = true
	}
	assert.Len(t, uniquePerms, 2, "Should have 2 unique permission types (read and write)")
}

func TestLegacyRuleWithInvalidPermissions(t *testing.T) {
	// Test handling of invalid permission types in legacy format
	// This documents behavior with unknown permission strings
	yamlContent := `
- path: "test/*"
  user: "testuser@example.com"
  permissions: ["read", "invalid_permission", "write"]
`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should parse even with invalid permissions")

	rule := legacyPerm.Rules[0]
	assert.Len(t, rule.Permissions, 3, "Should include all permission strings including invalid ones")
	assert.Contains(t, rule.Permissions, Read, "Should contain valid read permission")
	assert.Contains(t, rule.Permissions, Write, "Should contain valid write permission")
	assert.Contains(t, rule.Permissions, PermissionType("invalid_permission"), "Should preserve invalid permission as-is")
}

func TestLegacyPermissionComplexPaths(t *testing.T) {
	// Test legacy permission parsing with complex file paths
	// This ensures path handling works correctly for various path formats
	yamlContent := `
- path: "**/*.go"
  user: "developer@company.com"
  permissions: ["read", "write"]
- path: "/absolute/path/*"
  user: "admin@openmined.org"
  permissions: ["admin"]
- path: "relative/../path"
  user: "user@university.edu"
  permissions: ["read"]
- path: ""
  user: "empty_path_user@example.com"
  permissions: ["read"]
`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should handle complex paths")

	assert.Len(t, legacyPerm.Rules, 4, "Should parse all rules with complex paths")

	// Verify each path is preserved exactly as specified
	assert.Equal(t, "**/*.go", legacyPerm.Rules[0].Path, "Glob pattern should be preserved")
	assert.Equal(t, "/absolute/path/*", legacyPerm.Rules[1].Path, "Absolute path should be preserved")
	assert.Equal(t, "relative/../path", legacyPerm.Rules[2].Path, "Relative path with .. should be preserved")
	assert.Equal(t, "", legacyPerm.Rules[3].Path, "Empty path should be preserved")
}

func TestLegacyPermissionSpecialCharacters(t *testing.T) {
	// Test legacy permission parsing with special characters in user names and paths
	// This validates handling of edge cases in legacy data
	yamlContent := `
- path: "files with spaces/*"
  user: "user.with.dots"
  permissions: ["read"]
- path: "unicode/测试/*"
  user: "用户名"
  permissions: ["write"]
- path: "symbols!@#$%/*"
  user: "user_with_underscores"
  permissions: ["admin"]
`

	var legacyPerm LegacyPermission
	err := yaml.Unmarshal([]byte(yamlContent), &legacyPerm)
	require.NoError(t, err, "Should handle special characters in paths and users")

	assert.Len(t, legacyPerm.Rules, 3, "Should parse all rules with special characters")

	// Verify special characters are preserved
	assert.Equal(t, "files with spaces/*", legacyPerm.Rules[0].Path, "Spaces in paths should be preserved")
	assert.Equal(t, "user.with.dots", legacyPerm.Rules[0].User, "Dots in usernames should be preserved")
	
	assert.Equal(t, "unicode/测试/*", legacyPerm.Rules[1].Path, "Unicode characters should be preserved")
	assert.Equal(t, "用户名", legacyPerm.Rules[1].User, "Unicode usernames should be preserved")
	
	assert.Equal(t, "symbols!@#$%/*", legacyPerm.Rules[2].Path, "Symbol characters should be preserved")
	assert.Equal(t, "user_with_underscores", legacyPerm.Rules[2].User, "Underscores should be preserved")
}