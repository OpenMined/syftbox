package acl

import (
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
)

func TestNodeFindBestRule(t *testing.T) {
	// Create a node with some rules
	node := NewNode("test", false, 1)

	// Create test rules with different patterns
	rules := []*aclspec.Rule{
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("file.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("**/*.go", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	}

	node.SetRules(rules, false)

	// Test matching with different paths
	rule, err := node.FindBestRule("test/file.txt")
	assert.NoError(t, err)
	assert.Equal(t, "*.txt", rule.rule.Pattern)

	rule, err = node.FindBestRule("test/file.md")
	assert.NoError(t, err)
	assert.Equal(t, "file.md", rule.rule.Pattern)

	rule, err = node.FindBestRule("test/subdir/main.go")
	assert.NoError(t, err)
	assert.Equal(t, "**/*.go", rule.rule.Pattern)

	rule, err = node.FindBestRule("test/main.go")
	assert.NoError(t, err)
	assert.Equal(t, "**/*.go", rule.rule.Pattern)

	// Test non-matching path
	rule, err = node.FindBestRule("main.go")
	assert.Nil(t, rule)
	assert.Error(t, err)

	rule, err = node.FindBestRule("test/file.jpg")
	assert.Error(t, err)
	assert.Nil(t, rule)
}

func TestNodeSetRules(t *testing.T) {
	node := NewNode("test", false, 1)

	// Initial version should be 0
	assert.Equal(t, uint8(0), node.Version())

	// Set rules and check that version increments
	rules := []*aclspec.Rule{
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	}

	node.SetRules(rules, true)

	// Version should increment
	assert.Equal(t, uint8(1), node.Version())

	// Terminal flag should be set
	assert.True(t, node.IsTerminal())

	// Rules should be set
	assert.Len(t, node.Rules(), 1)

	// Set new rules and check version increments again
	newRules := []*aclspec.Rule{
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	}

	node.SetRules(newRules, false)

	// Version should increment
	assert.Equal(t, uint8(2), node.Version())

	// Terminal flag should be updated
	assert.False(t, node.IsTerminal())

	// Rules should be updated
	assert.Len(t, node.Rules(), 2)
}

func TestNodeEqual(t *testing.T) {
	node1 := NewNode("test", false, 1)
	node2 := NewNode("test", false, 1)
	node3 := NewNode("different", false, 1)
	node4 := NewNode("test", true, 1)
	node5 := NewNode("test", false, 2)

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
	sorted := sortBySpecificity(rules)

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
		score := globSpecificityScore(tc.pattern)
		assert.Equal(t, tc.score, score, "Specificity score for %q should be %d, got %d", tc.pattern, tc.score, score)
	}
}

func TestNodeGetChild(t *testing.T) {
	node := NewNode("parent", false, 1)

	// Initially, no children
	child, exists := node.GetChild("child")
	assert.False(t, exists)
	assert.Nil(t, child)

	// Add a child
	childNode := NewNode("parent/child", false, 2)
	node.SetChild("child", childNode)

	// Verify child can be retrieved
	child, exists = node.GetChild("child")
	assert.True(t, exists)
	assert.Equal(t, "parent/child", child.path)

	// Delete the child
	node.DeleteChild("child")

	// Verify child is gone
	child, exists = node.GetChild("child")
	assert.False(t, exists)
	assert.Nil(t, child)
}
