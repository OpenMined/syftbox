package acl

import (
	"strings"
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
)

func TestCalculateGlobSpecificity(t *testing.T) {
	tests := []struct {
		pattern     string
		expected    int
		description string
	}{
		// Special cases
		{"**", -100, "universal pattern"},
		{"**/*", -99, "universal pattern with slash"},

		// Basic patterns
		{"", 0, "empty pattern"},
		{"/", 12, "root slash"},
		{"///", 36, "multiple slashes"},

		// Simple files
		{"file.txt", 16, "simple file"},
		{"document.pdf", 24, "simple document"},

		// Directory patterns
		{"folder/file.txt", 40, "single directory"},
		{"a/b/c/d/file.txt", 72, "deep directory structure"},

		// Wildcard patterns
		{"*.txt", -10, "file extension wildcard"},
		{"?file.txt", 16, "single character wildcard"},
		{"[abc].txt", 16, "character class wildcard"},
		{"file?.txt", 16, "single character wildcard"},

		// Multiple wildcards
		{"**/*.txt", -14, "deep wildcard with extension"},
		{"a*/b*/c*", 6, "multiple wildcards in directories"},
		{"file*.txt", 8, "wildcard in filename"},

		// Leading wildcards (highest penalty)
		{"*file.txt", -2, "leading wildcard"},
		{"**/file.txt", 2, "leading deep wildcard"},
		{"*/file.txt", 10, "leading single wildcard"},

		// Template patterns (get +50 bonus)
		{"{{.UserEmail}}/*", 78, "simple template"},
		{"user_{{.UserEmail}}/**", 80, "template with deep wildcard"},
		{"{{.Year}}/{{.Month}}/*", 96, "multiple template variables"},
		{"{{.UserEmail}}/{{.Year}}/*.txt", 112, "template with extension"},

		// Complex nested templates
		{"alice@email.com/ben@email.com/{{.UserEmail}}/*.txt", 166, "nested template with static segments"},
		{"{{.Year}}/{{.Month}}/{{.UserEmail}}/**/*.csv", 136, "complex template with deep glob"},
		{"user_{{.UserHash}}/{{.UserEmail}}/*", 122, "template with hash and email"},

		// Mixed complexity
		{"public/*.txt", 24, "public directory with extension"},
		{"private/**/*.md", 20, "private with deep markdown"},
		{"shared/{{.UserEmail}}/*.pdf", 110, "shared with user template"},

		// Wild mixed template patterns
		{"{{.UserEmail}}/pvt_{{.UserHash}}/*", 120, "email template with hash subdirectory"},
		{"{{.UserHash}}/{{.UserEmail}}/{{.Year}}/*", 138, "hash + email + year template"},
		{"{{.Year}}/{{.Month}}/{{.UserEmail}}/{{.UserHash}}/*.txt", 174, "time + user + hash template"},
		{"user_{{.UserHash}}/{{.UserEmail}}/{{.Date}}/*", 148, "hash + email + date template"},
		{"{{.UserEmail}}/{{.Year}}/{{.Month}}/{{.Date}}/**", 150, "full user + time template"},
		{"{{.UserHash}}/{{.UserEmail}}/{{.Year}}/{{.Month}}/*.csv", 174, "hash + email + year + month template"},
		{"{{.UserEmail}}/{{.UserHash}}/{{.Year}}/{{.Month}}/{{.Date}}/*", 192, "full user + full time template"},
		{"{{.UserEmail}}/{{.UserHash}}/{{.Year}}/{{.Month}}/{{.Date}}/**/*.txt", 196, "full user + full time + deep glob"},

		// Template with mixed wildcards and static segments
		{"{{.UserEmail}}/static_folder/*.txt", 124, "template + static + extension"},
		{"static_prefix/{{.UserEmail}}/{{.UserHash}}/*", 150, "static + template + template"},
		{"{{.UserEmail}}/{{.UserHash}}/static_suffix/*.pdf", 158, "template + template + static + extension"},
		{"prefix_{{.UserEmail}}/{{.UserHash}}_suffix/*", 140, "static prefix + template + static suffix"},

		// Complex nested static + template combinations
		{"alice@email.com/{{.UserEmail}}/ben@email.com/{{.UserHash}}/*", 192, "static + template + static + template"},
		{"{{.Year}}/company_{{.UserEmail}}/{{.Month}}/user_{{.UserHash}}/*", 192, "time + static + template + time + static + template"},
		{"admin_{{.UserEmail}}/{{.Year}}/{{.Month}}/{{.UserHash}}/{{.Date}}/*", 204, "static + template + time + template + time"},

		// Template with regex-like patterns
		{"{{.UserEmail}}/[abc]*.txt", 94, "template + character class + wildcard"},
		{"{{.UserHash}}/*[xyz].pdf", 92, "template + wildcard + character class"},
		{"{{.Year}}/{{.Month}}/?{{.UserEmail}}/*", 132, "time + single char + template + wildcard"},

		// Ultra complex patterns (edge cases)
		{"{{.UserEmail}}/{{.UserHash}}/{{.Year}}/{{.Month}}/{{.Date}}/{{.UserEmail}}/*", 228, "circular template reference"},
		{"{{.UserEmail}}/{{.UserHash}}/{{.Year}}/{{.Month}}/{{.Date}}/{{.UserHash}}/*", 226, "repeated hash template"},
		{"{{.UserEmail}}/{{.UserHash}}/{{.Year}}/{{.Month}}/{{.Date}}/{{.Year}}/*", 218, "repeated year template"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := calculateGlobSpecificity(tt.pattern)
			assert.Equal(t, tt.expected, result,
				"Pattern: %s, Expected: %d, Got: %d",
				tt.pattern, tt.expected, result)
		})
	}
}

func TestSortRulesBySpecificity(t *testing.T) {
	// Helper function to create test rules
	createRule := func(pattern string) *aclspec.Rule {
		return &aclspec.Rule{
			Pattern: pattern,
			Access:  aclspec.PrivateAccess(),
		}
	}

	// Helper function to extract patterns from rules
	extractPatterns := func(rules []*aclspec.Rule) []string {
		patterns := make([]string, len(rules))
		for i, rule := range rules {
			patterns[i] = rule.Pattern
		}
		return patterns
	}

	tests := []struct {
		input       []*aclspec.Rule
		expected    []string
		description string
	}{
		// Empty and single element cases
		{
			[]*aclspec.Rule{},
			[]string{},
			"empty rules slice",
		},
		{
			[]*aclspec.Rule{createRule("**")},
			[]string{"**"},
			"single rule",
		},

		// Basic sorting by specificity
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("*.txt"),
				createRule("file.txt"),
			},
			[]string{"file.txt", "*.txt", "**"},
			"basic specificity sorting",
		},

		// Directory depth sorting
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("a/**"),
				createRule("a/b/**"),
				createRule("a/b/c/**"),
			},
			[]string{"a/b/c/**", "a/b/**", "a/**", "**"},
			"directory depth sorting",
		},

		// Template priority over globs
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("*.txt"),
				createRule("{{.UserEmail}}/*"),
				createRule("public/*"),
			},
			[]string{"{{.UserEmail}}/*", "public/*", "*.txt", "**"},
			"template priority over globs",
		},

		// Complex mixed patterns
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("*.txt"),
				createRule("public/*.txt"),
				createRule("public/data.csv"),
				createRule("{{.UserEmail}}/*.txt"),
				createRule("public/**/*.csv"),
			},
			[]string{
				"{{.UserEmail}}/*.txt",
				"public/data.csv",
				"public/*.txt",
				"public/**/*.csv",
				"*.txt",
				"**",
			},
			"complex mixed pattern sorting",
		},

		// Duplicate patterns (should maintain order)
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("**"),
				createRule("*.txt"),
			},
			[]string{"*.txt", "**", "**"},
			"duplicate patterns maintain order",
		},

		// Nested template complexity
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("alice@email.com/ben@email.com/{{.UserEmail}}/*.txt"),
				createRule("{{.UserEmail}}/*"),
				createRule("public/*.txt"),
			},
			[]string{
				"alice@email.com/ben@email.com/{{.UserEmail}}/*.txt",
				"{{.UserEmail}}/*",
				"public/*.txt",
				"**",
			},
			"nested template complexity sorting",
		},

		// Wild mixed template patterns sorting
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("{{.UserEmail}}/pvt_{{.UserHash}}/*"),
				createRule("{{.UserHash}}/{{.UserEmail}}/{{.Year}}/*"),
				createRule("{{.Year}}/{{.Month}}/{{.UserEmail}}/{{.UserHash}}/*.txt"),
				createRule("{{.UserEmail}}/{{.UserHash}}/{{.Year}}/{{.Month}}/{{.Date}}/*"),
				createRule("*.txt"),
			},
			[]string{
				"{{.UserEmail}}/{{.UserHash}}/{{.Year}}/{{.Month}}/{{.Date}}/*",
				"{{.Year}}/{{.Month}}/{{.UserEmail}}/{{.UserHash}}/*.txt",
				"{{.UserHash}}/{{.UserEmail}}/{{.Year}}/*",
				"{{.UserEmail}}/pvt_{{.UserHash}}/*",
				"*.txt",
				"**",
			},
			"wild mixed template patterns sorting",
		},

		// Complex static + template combinations sorting
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("alice@email.com/{{.UserEmail}}/ben@email.com/{{.UserHash}}/*"),
				createRule("{{.Year}}/company_{{.UserEmail}}/{{.Month}}/user_{{.UserHash}}/*"),
				createRule("admin_{{.UserEmail}}/{{.Year}}/{{.Month}}/{{.UserHash}}/{{.Date}}/*"),
				createRule("{{.UserEmail}}/{{.UserHash}}/static_suffix/*.pdf"),
				createRule("public/*.txt"),
			},
			[]string{
				"admin_{{.UserEmail}}/{{.Year}}/{{.Month}}/{{.UserHash}}/{{.Date}}/*",
				"alice@email.com/{{.UserEmail}}/ben@email.com/{{.UserHash}}/*",
				"{{.Year}}/company_{{.UserEmail}}/{{.Month}}/user_{{.UserHash}}/*",
				"{{.UserEmail}}/{{.UserHash}}/static_suffix/*.pdf",
				"public/*.txt",
				"**",
			},
			"complex static + template combinations sorting",
		},

		// Template with regex-like patterns sorting
		{
			[]*aclspec.Rule{
				createRule("**"),
				createRule("{{.UserEmail}}/[abc]*.txt"),
				createRule("{{.Hash}}/*[xyz].pdf"),
				createRule("{{.Year}}/{{.Month}}/?{{.UserEmail}}/*"),
				createRule("*.txt"),
				createRule("public/*"),
			},
			[]string{
				"{{.Year}}/{{.Month}}/?{{.UserEmail}}/*",
				"{{.UserEmail}}/[abc]*.txt",
				"{{.Hash}}/*[xyz].pdf",
				"public/*",
				"*.txt",
				"**",
			},
			"template with regex-like patterns sorting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := sortRulesBySpecificity(tt.input)
			patterns := extractPatterns(result)
			assert.Equal(t, tt.expected, patterns,
				"Expected: %v, Got: %v", tt.expected, patterns)
		})
	}
}

func TestSortRulesBySpecificityStability(t *testing.T) {
	// Test that sorting is stable (same scores maintain order)
	createRule := func(pattern string) *aclspec.Rule {
		return &aclspec.Rule{
			Pattern: pattern,
			Access:  aclspec.PrivateAccess(),
		}
	}

	// Rules with same specificity score
	rules := []*aclspec.Rule{
		createRule("a/file.txt"), // Score: 2(9) + 10(1) = 28
		createRule("b/file.txt"), // Score: 2(9) + 10(1) = 28
		createRule("c/file.txt"), // Score: 2(9) + 10(1) = 28
	}

	result := sortRulesBySpecificity(rules)

	// All should have same score, so order should be preserved
	assert.Equal(t, "a/file.txt", result[0].Pattern)
	assert.Equal(t, "b/file.txt", result[1].Pattern)
	assert.Equal(t, "c/file.txt", result[2].Pattern)
}

func TestCalculateGlobSpecificityEdgeCases(t *testing.T) {
	// Test with very long patterns
	longPattern := strings.Repeat("a/", 100) + "file.txt"
	score := calculateGlobSpecificity(longPattern)
	assert.Greater(t, score, 0, "Very long pattern should have positive score")

	// Test with special characters
	specialPattern := "file[!abc].txt"
	score = calculateGlobSpecificity(specialPattern)
	assert.Greater(t, score, 0, "Special character pattern should have positive score")

	// Test with mixed wildcards
	mixedPattern := "a*/b?/c[abc]/*.txt"
	score = calculateGlobSpecificity(mixedPattern)
	assert.Less(t, score, 100, "Mixed wildcard pattern should have reasonable score")
}
