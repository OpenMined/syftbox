package aclspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRule(t *testing.T) {
	// Test creating a new Rule with all components
	// This validates the core Rule constructor works correctly
	pattern := "*.txt"
	access := PublicReadAccess()
	limits := DefaultLimits()

	rule := NewRule(pattern, access, limits)

	// Verify all components are properly assigned
	assert.NotNil(t, rule, "NewRule should return a non-nil Rule")
	assert.Equal(t, pattern, rule.Pattern, "Pattern should be preserved")
	assert.Equal(t, access, rule.Access, "Access should be preserved")
	assert.Equal(t, limits, rule.Limits, "Limits should be preserved")
}

func TestNewRuleWithDifferentPatterns(t *testing.T) {
	// Test creating rules with various pattern types
	// This ensures the constructor handles different glob patterns correctly
	testCases := []struct {
		pattern string
		desc    string
	}{
		{
			pattern: "**",
			desc:    "Universal pattern should be preserved",
		},
		{
			pattern: "*.go",
			desc:    "Extension-based pattern should be preserved",
		},
		{
			pattern: "specific.txt",
			desc:    "Specific filename pattern should be preserved",
		},
		{
			pattern: "folder/**/*.py",
			desc:    "Complex nested pattern should be preserved",
		},
		{
			pattern: "",
			desc:    "Empty pattern should be preserved (even if invalid)",
		},
	}

	access := PrivateAccess()
	limits := DefaultLimits()

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			rule := NewRule(tc.pattern, access, limits)
			assert.Equal(t, tc.pattern, rule.Pattern, tc.desc)
		})
	}
}

func TestNewRuleWithDifferentAccess(t *testing.T) {
	// Test creating rules with different access configurations
	// This validates rules can be created with various permission levels
	pattern := "test.txt"
	limits := DefaultLimits()

	testCases := []struct {
		access *Access
		desc   string
	}{
		{
			access: PrivateAccess(),
			desc:   "Private access should be preserved",
		},
		{
			access: PublicReadAccess(),
			desc:   "Public read access should be preserved",
		},
		{
			access: PublicReadWriteAccess(),
			desc:   "Public read-write access should be preserved",
		},
		{
			access: SharedReadAccess("user1", "user2"),
			desc:   "Shared read access should be preserved",
		},
		{
			access: SharedReadWriteAccess("maintainer"),
			desc:   "Shared read-write access should be preserved",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			rule := NewRule(pattern, tc.access, limits)
			assert.Equal(t, tc.access, rule.Access, tc.desc)
		})
	}
}

func TestNewRuleWithDifferentLimits(t *testing.T) {
	// Test creating rules with different limit configurations
	// This validates rules can enforce various restrictions
	pattern := "test.txt"
	access := PrivateAccess()

	testCases := []struct {
		limits *Limits
		desc   string
	}{
		{
			limits: DefaultLimits(),
			desc:   "Default limits should be preserved",
		},
		{
			limits: &Limits{
				MaxFileSize:   1024,
				MaxFiles:      10,
				AllowDirs:     false,
				AllowSymlinks: false,
			},
			desc: "Custom restrictive limits should be preserved",
		},
		{
			limits: &Limits{
				MaxFileSize:   0, // Unlimited
				MaxFiles:      0, // Unlimited
				AllowDirs:     true,
				AllowSymlinks: true,
			},
			desc: "Custom permissive limits should be preserved",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			rule := NewRule(pattern, access, tc.limits)
			assert.Equal(t, tc.limits, rule.Limits, tc.desc)
		})
	}
}

func TestNewRuleWithNilInputs(t *testing.T) {
	// Test creating rules with nil inputs
	// This documents behavior with invalid inputs (system should handle gracefully)
	pattern := "test.txt"

	// Test with nil access (this might be invalid but shouldn't crash)
	rule := NewRule(pattern, nil, DefaultLimits())
	assert.Equal(t, pattern, rule.Pattern, "Pattern should be preserved even with nil access")
	assert.Nil(t, rule.Access, "Nil access should be preserved")
	assert.NotNil(t, rule.Limits, "Limits should be preserved")

	// Test with nil limits (this might be invalid but shouldn't crash)
	rule = NewRule(pattern, PrivateAccess(), nil)
	assert.Equal(t, pattern, rule.Pattern, "Pattern should be preserved even with nil limits")
	assert.NotNil(t, rule.Access, "Access should be preserved")
	assert.Nil(t, rule.Limits, "Nil limits should be preserved")
}

func TestNewDefaultRule(t *testing.T) {
	// Test creating a default rule (catch-all pattern)
	// This validates the convenience constructor for the most common default rule
	access := PrivateAccess()
	limits := DefaultLimits()

	rule := NewDefaultRule(access, limits)

	// Verify the rule uses the universal pattern
	assert.NotNil(t, rule, "NewDefaultRule should return a non-nil Rule")
	assert.Equal(t, AllFiles, rule.Pattern, "Default rule should use the AllFiles pattern (**)")
	assert.Equal(t, access, rule.Access, "Access should be preserved")
	assert.Equal(t, limits, rule.Limits, "Limits should be preserved")
}

func TestNewDefaultRuleWithDifferentAccess(t *testing.T) {
	// Test default rule creation with various access levels
	// This ensures default rules can be created with any permission configuration
	limits := DefaultLimits()

	testCases := []struct {
		access *Access
		desc   string
	}{
		{
			access: PrivateAccess(),
			desc:   "Default rule with private access",
		},
		{
			access: PublicReadAccess(),
			desc:   "Default rule with public read access",
		},
		{
			access: SharedReadWriteAccess("admin"),
			desc:   "Default rule with shared access",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			rule := NewDefaultRule(tc.access, limits)
			assert.Equal(t, AllFiles, rule.Pattern, "All default rules should use AllFiles pattern")
			assert.Equal(t, tc.access, rule.Access, tc.desc)
		})
	}
}

func TestRulePatternConstants(t *testing.T) {
	// Test that rules correctly use the expected pattern constants
	// This ensures consistency in pattern usage across the system
	
	// Default rule should use AllFiles constant
	defaultRule := NewDefaultRule(PrivateAccess(), DefaultLimits())
	assert.Equal(t, "**", defaultRule.Pattern, "Default rule should use the AllFiles constant value")
	assert.Equal(t, AllFiles, defaultRule.Pattern, "Default rule should use AllFiles constant")
	
	// Custom rule should preserve custom pattern
	customRule := NewRule("custom/*.txt", PrivateAccess(), DefaultLimits())
	assert.NotEqual(t, AllFiles, customRule.Pattern, "Custom rule should not use AllFiles pattern")
	assert.Equal(t, "custom/*.txt", customRule.Pattern, "Custom rule should preserve its pattern")
}

func TestRuleIndependence(t *testing.T) {
	// Test that multiple rules are independent objects
	// This ensures modifying one rule doesn't affect others
	access1 := PrivateAccess()
	access2 := PublicReadAccess()
	limits1 := DefaultLimits()
	limits2 := &Limits{MaxFileSize: 1000}

	rule1 := NewRule("*.txt", access1, limits1)
	rule2 := NewRule("*.md", access2, limits2)

	// Verify rules are independent
	assert.NotEqual(t, rule1.Pattern, rule2.Pattern, "Rules should have different patterns")
	assert.NotEqual(t, rule1.Access, rule2.Access, "Rules should have different access")
	assert.NotEqual(t, rule1.Limits, rule2.Limits, "Rules should have different limits")

	// Verify shared objects are actually the same references when intended
	rule3 := NewRule("*.go", access1, limits1)
	assert.Equal(t, rule1.Access, rule3.Access, "Rules sharing the same access object should reference the same object")
	assert.Equal(t, rule1.Limits, rule3.Limits, "Rules sharing the same limits object should reference the same object")
}