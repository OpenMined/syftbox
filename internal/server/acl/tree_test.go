package acl

import (
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
)

func TestNewTree(t *testing.T) {
	tree := NewTree()
	assert.NotNil(t, tree)
	assert.NotNil(t, tree.root)
	assert.Equal(t, "/", tree.root.path)
	assert.Equal(t, pathSep, tree.root.path)
	assert.Empty(t, tree.root.children)
	assert.Empty(t, tree.root.rules)
}

func TestAddRuleSet(t *testing.T) {
	tree := NewTree()

	ruleset := aclspec.NewRuleSet(
		"test/path",
		aclspec.UnsetTerminal,
		aclspec.NewDefaultRule(aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := tree.AddRuleSet(ruleset)

	// check root node "/"
	assert.NoError(t, err)
	assert.Empty(t, tree.root.rules)
	assert.Contains(t, tree.root.children, "test")
	assert.Equal(t, tree.root.path, "/")
	assert.Equal(t, tree.root.depth, uint8(0))

	// check node "test"
	child, ok := tree.root.GetChild("test")
	assert.True(t, ok)
	assert.NotNil(t, child)
	assert.Empty(t, child.rules)
	assert.Contains(t, child.children, "path")
	assert.Equal(t, child.path, "test")
	assert.Equal(t, child.depth, uint8(1))

	// check node "path"
	child, ok = child.GetChild("path")
	assert.True(t, ok)
	assert.NotNil(t, child)
	assert.Equal(t, child.path, "test/path")
	assert.Equal(t, child.depth, uint8(2))
}

func TestTreeTraversal(t *testing.T) {
	tree := NewTree()

	// Add rulesets with nested paths
	ruleset1 := aclspec.NewRuleSet(
		"parent",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"parent/child",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ruleset3 := aclspec.NewRuleSet(
		"parent/child/grandchild",
		aclspec.SetTerminal,
		aclspec.NewRule("*.go", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	err := tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	err = tree.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	err = tree.AddRuleSet(ruleset3)
	assert.NoError(t, err)

	// Test finding nearest node with rules for different paths
	node := tree.GetNearestNodeWithRules("parent/file.txt")
	assert.Equal(t, "parent", node.path)

	node = tree.GetNearestNodeWithRules("parent/child/document.md")
	assert.Equal(t, "parent/child", node.path)

	node = tree.GetNearestNodeWithRules("parent/child/grandchild/main.go")
	assert.Equal(t, "parent/child", node.path)

	// Test inheritance - terminal nodes (like parent/child) block inheritance from higher levels
	node = tree.GetNearestNodeWithRules("parent/child/unknown.txt")
	assert.Equal(t, "parent/child", node.path)

	// Test path that doesn't exist in the tree
	node = tree.GetNearestNodeWithRules("unknown/path")
	assert.Nil(t, node)
}

func TestRemoveRuleSet(t *testing.T) {
	tree := NewTree()

	// Add rulesets
	ruleset1 := aclspec.NewRuleSet(
		"folder1",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"folder2",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	err = tree.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	// Verify both rulesets are in the tree
	_, ok := tree.root.GetChild("folder1")
	assert.True(t, ok)

	_, ok = tree.root.GetChild("folder2")
	assert.True(t, ok)

	// Remove one ruleset
	removed := tree.RemoveRuleSet("folder1")
	assert.True(t, removed)

	// Verify it was removed
	_, ok = tree.root.GetChild("folder1")
	assert.False(t, ok)

	// Verify other ruleset is still present
	_, ok = tree.root.GetChild("folder2")
	assert.True(t, ok)

	// Try to remove non-existent ruleset
	removed = tree.RemoveRuleSet("non-existent")
	assert.False(t, removed)
}

func TestNestedRuleSetRemoval(t *testing.T) {
	tree := NewTree()

	// Add nested rulesets
	ruleset1 := aclspec.NewRuleSet(
		"parent",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"parent/child",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	err = tree.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	// Remove parent - should also remove child
	removed := tree.RemoveRuleSet("parent")
	assert.True(t, removed)

	// Verify both are gone
	_, ok := tree.root.GetChild("parent")
	assert.False(t, ok)

	// Add the parent ruleset back
	err = tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	// Add the child ruleset back
	err = tree.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	// Remove just the child
	removed = tree.RemoveRuleSet("parent/child")
	assert.True(t, removed)

	// Verify parent still exists
	parentNode, ok := tree.root.GetChild("parent")
	assert.True(t, ok)
	assert.NotNil(t, parentNode)

	// Verify child was removed
	_, ok = parentNode.GetChild("child")
	assert.False(t, ok)
}
