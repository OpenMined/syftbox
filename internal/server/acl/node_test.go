package acl

import (
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
)

func TestNodeRuleMatching(t *testing.T) {
	// Create a node with some rules
	node := NewACLNode("some/path", "user1", false, 1)

	// Create test rules with different patterns
	rules := []*aclspec.Rule{
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("file.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("**/*.go", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	}

	node.SetRules(rules, false)

	// Test that rules are set correctly
	aclRules := node.GetRules()
	assert.Len(t, aclRules, 3)

	// Test that rules exist by checking the patterns
	patterns := make([]string, len(aclRules))
	for i, rule := range aclRules {
		patterns[i] = rule.rule.Pattern
	}
	assert.Contains(t, patterns, "*.txt")
	assert.Contains(t, patterns, "file.md")
	assert.Contains(t, patterns, "**/*.go")
}

func TestNodeSetRules(t *testing.T) {
	node := NewACLNode("test", "user1", false, 1)

	// Initial version should be 0
	assert.Equal(t, ACLVersion(0), node.GetVersion())

	// Set rules and check that version increments
	rules := []*aclspec.Rule{
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	}

	node.SetRules(rules, true)

	// Version should increment
	assert.Equal(t, ACLVersion(1), node.GetVersion())

	// Terminal flag should be set
	assert.True(t, node.GetTerminal())

	// Rules should be set
	assert.Len(t, node.GetRules(), 1)

	// Set new rules and check version increments again
	newRules := []*aclspec.Rule{
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	}

	node.SetRules(newRules, false)

	// Version should increment
	assert.Equal(t, ACLVersion(2), node.GetVersion())

	// Terminal flag should be updated
	assert.False(t, node.GetTerminal())

	// Rules should be updated
	assert.Len(t, node.GetRules(), 2)
}

func TestNodeEqual(t *testing.T) {
	node1 := NewACLNode("some/path", "user1", false, 1)
	node2 := NewACLNode("some/path", "user1", false, 1)
	node3 := NewACLNode("different/path", "user1", false, 1)
	node4 := NewACLNode("some/path", "user1", true, 1)
	node5 := NewACLNode("some/path", "user1", false, 2)

	assert.True(t, node1.Equal(node2), "Identical nodes should be equal")
	assert.False(t, node1.Equal(node3), "Nodes with different paths should not be equal")
	assert.False(t, node1.Equal(node4), "Nodes with different terminal flags should not be equal")
	assert.False(t, node1.Equal(node5), "Nodes with different depths should not be equal")
}

func TestRuleSpecificity(t *testing.T) {
	// Test cases for rule specificity
	rules := []*aclspec.Rule{
		aclspec.NewRule("**", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("**/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("specific.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("folder/*.go", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	}

	// Sort by specificity
	sorted := sortRulesBySpecificity(rules)

	// Most specific should come first, least specific last
	assert.Equal(t, "specific.txt", sorted[0].Pattern)
	assert.Equal(t, "folder/*.go", sorted[1].Pattern)
	assert.Equal(t, "*.txt", sorted[2].Pattern)
	assert.Equal(t, "**", sorted[len(sorted)-1].Pattern, "The '**' pattern should be least specific")
}

func TestGlobSpecificityScore(t *testing.T) {
	testCases := []struct {
		pattern string
		score   int
	}{
		{"**", -100},
		{"**/*", -99},
		{"*.txt", -10},
		{"specific.txt", 24},
		{"specific2.txt", 26},
		{"path/to/**/specific.txt", 56},
	}

	for _, tc := range testCases {
		t.Run(tc.pattern, func(t *testing.T) {
			score := calculateGlobSpecificity(tc.pattern)
			assert.Equal(t, tc.score, score, "Specificity score for '%s' should be %d, got %d", tc.pattern, tc.score, score)
		})
	}
}

func TestNodeGetChild(t *testing.T) {
	node := NewACLNode("path/to", "user1", false, 1)

	// Initially, no children
	child, exists := node.GetChild("child")
	assert.False(t, exists)
	assert.Nil(t, child)

	// Add a child
	childNode := NewACLNode("path/to/child", "user1", false, 2)
	node.SetChild("child", childNode)

	// Verify child can be retrieved
	child, exists = node.GetChild("child")
	assert.True(t, exists)
	assert.Equal(t, "path/to/child", child.path)

	// Delete the child
	node.DeleteChild("child")

	// Verify child is gone
	child, exists = node.GetChild("child")
	assert.False(t, exists)
	assert.Nil(t, child)
}
