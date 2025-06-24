package aclspec

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewRuleSet(t *testing.T) {
	// Test creating a new RuleSet with basic configuration
	// This validates the core RuleSet constructor
	path := "test/path"
	terminal := true
	rule1 := NewRule("*.txt", PublicReadAccess(), DefaultLimits())
	rule2 := NewRule("*.md", PrivateAccess(), DefaultLimits())

	ruleset := NewRuleSet(path, terminal, rule1, rule2)

	// Verify all components are properly assigned
	assert.NotNil(t, ruleset, "NewRuleSet should return a non-nil RuleSet")
	assert.Equal(t, path, ruleset.Path, "Path should be preserved")
	assert.Equal(t, terminal, ruleset.Terminal, "Terminal flag should be preserved")
	assert.Len(t, ruleset.Rules, 2, "Should have exactly 2 rules")
	assert.Contains(t, ruleset.Rules, rule1, "Should contain first rule")
	assert.Contains(t, ruleset.Rules, rule2, "Should contain second rule")
}

func TestNewRuleSetWithAclPath(t *testing.T) {
	// Test that NewRuleSet correctly handles ACL file paths
	// This ensures the constructor normalizes paths by removing ACL filename
	aclPath := "test/path/syft.pub.yaml"
	expectedPath := "test/path/"

	ruleset := NewRuleSet(aclPath, false)

	assert.Equal(t, expectedPath, ruleset.Path, "ACL filename should be stripped from path")
}

func TestNewRuleSetWithNoRules(t *testing.T) {
	// Test creating a RuleSet with no initial rules
	// This validates the constructor works with empty rule sets
	ruleset := NewRuleSet("test/path", false)

	assert.Equal(t, "test/path", ruleset.Path)
	assert.False(t, ruleset.Terminal)
	assert.Empty(t, ruleset.Rules, "RuleSet with no rules should have empty Rules slice")
}

func TestAllRules(t *testing.T) {
	// Test the AllRules method returns the correct rules
	// This is a simple getter but important for the interface
	rule1 := NewRule("*.txt", PublicReadAccess(), DefaultLimits())
	rule2 := NewRule("*.md", PrivateAccess(), DefaultLimits())
	ruleset := NewRuleSet("test", false, rule1, rule2)

	rules := ruleset.AllRules()

	assert.Len(t, rules, 2, "AllRules should return all rules")
	assert.Contains(t, rules, rule1, "Should contain first rule")
	assert.Contains(t, rules, rule2, "Should contain second rule")
}

func TestLoadFromReader(t *testing.T) {
	// Test loading RuleSet from YAML reader
	// This validates the core YAML parsing functionality
	yamlContent := `
terminal: true
rules:
  - pattern: "*.txt"
    access:
      read: ["*"]
  - pattern: "private/*"
    access:
      read: ["admin"]
      write: ["admin"]
`

	reader := io.NopCloser(strings.NewReader(yamlContent))
	ruleset, err := LoadFromReader("test/path", reader)

	require.NoError(t, err, "LoadFromReader should succeed with valid YAML")
	assert.NotNil(t, ruleset, "Should return a non-nil RuleSet")

	// Verify basic properties
	assert.Equal(t, "test/path", ruleset.Path, "Path should be set correctly")
	assert.True(t, ruleset.Terminal, "Terminal flag should be parsed correctly")

	// Should have the 2 explicit rules plus 1 default rule added by setDefaults
	assert.Len(t, ruleset.Rules, 3, "Should have 2 explicit rules + 1 default rule")

	// Verify the first rule
	rule1 := ruleset.Rules[0]
	assert.Equal(t, "*.txt", rule1.Pattern, "First rule pattern should be correct")
	assert.True(t, rule1.Access.Read.Contains("*"), "First rule should grant read access to everyone")

	// Verify the second rule
	rule2 := ruleset.Rules[1]
	assert.Equal(t, "private/*", rule2.Pattern, "Second rule pattern should be correct")
	assert.True(t, rule2.Access.Read.Contains("admin"), "Second rule should grant read access to admin")
	assert.True(t, rule2.Access.Write.Contains("admin"), "Second rule should grant write access to admin")
}

func TestLoadFromReaderWithMinimalYAML(t *testing.T) {
	// Test loading with minimal YAML (only terminal flag)
	// This validates default rule injection works correctly
	yamlContent := `terminal: false`

	reader := io.NopCloser(strings.NewReader(yamlContent))
	ruleset, err := LoadFromReader("test", reader)

	require.NoError(t, err, "LoadFromReader should succeed with minimal YAML")
	assert.False(t, ruleset.Terminal, "Terminal flag should be parsed correctly")

	// Should have exactly 1 default rule added by setDefaults
	assert.Len(t, ruleset.Rules, 1, "Should have 1 default rule")
	assert.Equal(t, "**", ruleset.Rules[0].Pattern, "Default rule should have AllFiles pattern")
}

func TestLoadFromReaderWithEmptyYAML(t *testing.T) {
	// Test loading with completely empty YAML
	// This validates the system handles empty files gracefully
	yamlContent := ``

	reader := io.NopCloser(strings.NewReader(yamlContent))
	ruleset, err := LoadFromReader("test", reader)

	require.NoError(t, err, "LoadFromReader should succeed with empty YAML")
	assert.False(t, ruleset.Terminal, "Terminal should default to false")

	// Should have exactly 1 default rule added by setDefaults
	assert.Len(t, ruleset.Rules, 1, "Should have 1 default rule")
	assert.Equal(t, "**", ruleset.Rules[0].Pattern, "Default rule should have AllFiles pattern")
}

func TestLoadFromReaderWithInvalidYAML(t *testing.T) {
	// Test that invalid YAML is properly rejected
	// This ensures the system fails safely on malformed input
	yamlContent := `
invalid: yaml: content:
  - missing
    proper: structure
`

	reader := io.NopCloser(strings.NewReader(yamlContent))
	ruleset, err := LoadFromReader("test", reader)

	assert.Error(t, err, "LoadFromReader should fail with invalid YAML")
	assert.Nil(t, ruleset, "Should return nil RuleSet on error")
}

func TestLoadFromFile(t *testing.T) {
	// Test loading RuleSet from file on disk
	// This validates file I/O integration
	tempDir := t.TempDir()
	aclFile := filepath.Join(tempDir, AclFileName)

	yamlContent := `
terminal: true
rules:
  - pattern: "*.go"
    access:
      read: ["developers@company.com"]
`

	err := os.WriteFile(aclFile, []byte(yamlContent), 0644)
	require.NoError(t, err, "Should be able to write test file")

	ruleset, err := LoadFromFile(tempDir)
	require.NoError(t, err, "LoadFromFile should succeed")

	assert.Equal(t, tempDir, ruleset.Path, "Path should be directory (without ACL filename)")
	assert.True(t, ruleset.Terminal, "Terminal flag should be loaded correctly")
	assert.Len(t, ruleset.Rules, 2, "Should have 1 explicit rule + 1 default rule")
}

func TestLoadFromFileNonExistent(t *testing.T) {
	// Test loading from non-existent file
	// This validates error handling for missing files
	nonExistentPath := "/path/that/does/not/exist"

	ruleset, err := LoadFromFile(nonExistentPath)
	assert.Error(t, err, "LoadFromFile should fail for non-existent file")
	assert.Nil(t, ruleset, "Should return nil RuleSet on error")
}

func TestRuleSetSave(t *testing.T) {
	// Test saving RuleSet to file
	// This validates YAML serialization and file I/O
	tempDir := t.TempDir()

	// Create a RuleSet with test data
	rule1 := NewRule("*.txt", PublicReadAccess(), DefaultLimits())
	rule2 := NewRule("secret/*", PrivateAccess(), &Limits{MaxFileSize: 1024})
	ruleset := NewRuleSet(tempDir, true, rule1, rule2)

	err := ruleset.Save()
	require.NoError(t, err, "Save should succeed")

	// Verify the file was created
	aclFile := filepath.Join(tempDir, AclFileName)
	assert.FileExists(t, aclFile, "ACL file should be created")

	// Load the file back and verify content
	content, err := os.ReadFile(aclFile)
	require.NoError(t, err, "Should be able to read saved file")

	var loaded RuleSet
	err = yaml.Unmarshal(content, &loaded)
	require.NoError(t, err, "Saved YAML should be valid")

	assert.True(t, loaded.Terminal, "Terminal flag should be preserved")
	assert.Len(t, loaded.Rules, 2, "All rules should be saved")
}

func TestRuleSetSaveInvalidPath(t *testing.T) {
	// Test saving to invalid path
	// This validates error handling for file I/O failures
	ruleset := NewRuleSet("/invalid/path/that/cannot/be/created", false)

	err := ruleset.Save()
	assert.Error(t, err, "Save should fail for invalid path")
}

func TestSetDefaults(t *testing.T) {
	// Test the setDefaults function directly
	// This validates the default rule injection logic

	// Test with nil rules
	ruleset := &RuleSet{Path: "test", Terminal: false, Rules: nil}
	result, err := setDefaults(ruleset)

	require.NoError(t, err, "setDefaults should succeed with nil rules")
	assert.Len(t, result.Rules, 1, "Should add exactly one default rule")
	assert.Equal(t, "**", result.Rules[0].Pattern, "Default rule should have AllFiles pattern")
}

func TestSetDefaultsWithExistingDefaultRule(t *testing.T) {
	// Test setDefaults when a default rule already exists
	// This ensures default rules aren't duplicated
	existingDefault := NewRule("**", PrivateAccess(), DefaultLimits())
	customRule := NewRule("*.txt", PublicReadAccess(), DefaultLimits())

	ruleset := &RuleSet{
		Path:     "test",
		Terminal: false,
		Rules:    []*Rule{customRule, existingDefault},
	}

	result, err := setDefaults(ruleset)

	require.NoError(t, err, "setDefaults should succeed with existing default rule")
	assert.Len(t, result.Rules, 2, "Should not add additional default rule")

	// Verify the existing default rule is preserved
	hasDefault := false
	for _, rule := range result.Rules {
		if rule.Pattern == "**" {
			hasDefault = true
			break
		}
	}
	assert.True(t, hasDefault, "Should preserve existing default rule")
}

func TestSetDefaultsValidation(t *testing.T) {
	// Test setDefaults validation of rule requirements
	// This ensures invalid rules are properly rejected

	// Test with empty pattern
	invalidRule := NewRule("", PrivateAccess(), DefaultLimits())
	ruleset := &RuleSet{
		Path:     "test",
		Terminal: false,
		Rules:    []*Rule{invalidRule},
	}

	result, err := setDefaults(ruleset)
	assert.Error(t, err, "setDefaults should reject rules with empty patterns")
	assert.Nil(t, result, "Should return nil on validation error")

	// Test with nil access
	invalidRule2 := NewRule("*.txt", nil, DefaultLimits())
	ruleset2 := &RuleSet{
		Path:     "test",
		Terminal: false,
		Rules:    []*Rule{invalidRule2},
	}

	result, err = setDefaults(ruleset2)
	assert.Error(t, err, "setDefaults should reject rules with nil access")
	assert.Nil(t, result, "Should return nil on validation error")
}

func TestSetDefaultsLimitsInjection(t *testing.T) {
	// Test that setDefaults adds default limits to rules missing them
	// This ensures all rules have proper limits configuration
	ruleWithoutLimits := NewRule("*.txt", PrivateAccess(), nil)

	ruleset := &RuleSet{
		Path:     "test",
		Terminal: false,
		Rules:    []*Rule{ruleWithoutLimits},
	}

	result, err := setDefaults(ruleset)

	require.NoError(t, err, "setDefaults should succeed and inject limits")
	assert.NotNil(t, result.Rules[0].Limits, "Rule should have limits after setDefaults")

	// Verify the limits are the default limits
	expectedLimits := DefaultLimits()
	assert.Equal(t, expectedLimits.MaxFiles, result.Rules[0].Limits.MaxFiles)
	assert.Equal(t, expectedLimits.MaxFileSize, result.Rules[0].Limits.MaxFileSize)
	assert.Equal(t, expectedLimits.AllowDirs, result.Rules[0].Limits.AllowDirs)
	assert.Equal(t, expectedLimits.AllowSymlinks, result.Rules[0].Limits.AllowSymlinks)
}

func TestRuleSetRoundTrip(t *testing.T) {
	// Test complete round-trip: create -> save -> load -> verify
	// This validates the entire serialization/deserialization pipeline
	tempDir := t.TempDir()

	// Create original RuleSet
	original := NewRuleSet(tempDir, true,
		NewRule("*.go", SharedReadAccess("dev1@company.com", "dev2@university.edu"), DefaultLimits()),
		NewRule("docs/*", PublicReadAccess(), DefaultLimits()),
	)

	// Save to file
	err := original.Save()
	require.NoError(t, err, "Save should succeed")

	// Load from file
	loaded, err := LoadFromFile(tempDir)
	require.NoError(t, err, "Load should succeed")

	// Verify key properties are preserved
	assert.Equal(t, original.Path, loaded.Path, "Path should be preserved")
	assert.Equal(t, original.Terminal, loaded.Terminal, "Terminal flag should be preserved")

	// Note: loaded will have additional default rule, so we check the original rules exist
	assert.True(t, len(loaded.Rules) >= len(original.Rules), "Loaded rules should include all original rules")

	// Verify specific rules exist (order might differ due to default rule injection)
	hasGoRule := false
	hasDocsRule := false
	for _, rule := range loaded.Rules {
		if rule.Pattern == "*.go" {
			hasGoRule = true
			assert.True(t, rule.Access.Read.Contains("dev1@company.com"), "Go rule should preserve dev1 access")
			assert.True(t, rule.Access.Read.Contains("dev2@university.edu"), "Go rule should preserve dev2 access")
		}
		if rule.Pattern == "docs/*" {
			hasDocsRule = true
			assert.True(t, rule.Access.Read.Contains("*"), "Docs rule should preserve public access")
			// Note: Limits are not serialized to YAML (yaml:"-" tag), so they get default values after round-trip
			assert.NotNil(t, rule.Limits, "Docs rule should have default limits after round-trip")
		}
	}
	assert.True(t, hasGoRule, "Should preserve *.go rule")
	assert.True(t, hasDocsRule, "Should preserve docs/* rule")
}
