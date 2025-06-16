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
		aclspec.UnsetTerminal, // Changed to non-terminal so we can add grandchild
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
	assert.Equal(t, "parent/child/grandchild", node.path)

	// Test inheritance - terminal nodes (like grandchild) block inheritance from higher levels
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

func TestGetNode(t *testing.T) {
	// Test the GetNode method which finds exact nodes for given paths
	// This validates precise node location without rule inheritance logic
	tree := NewTree()

	// Add nested rulesets to create a tree structure
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

	// Test getting exact nodes that exist
	parentNode := tree.GetNode("parent")
	assert.NotNil(t, parentNode, "Should find parent node")
	assert.Equal(t, "parent", parentNode.path, "Parent node should have correct path")

	childNode := tree.GetNode("parent/child")
	assert.NotNil(t, childNode, "Should find child node")
	assert.Equal(t, "parent/child", childNode.path, "Child node should have correct path")

	// Test getting node for path that goes beyond existing nodes
	deepNode := tree.GetNode("parent/child/grandchild")
	// Should return the deepest existing node (child), not create new nodes
	assert.Equal(t, "parent/child", deepNode.path, "Should return deepest existing node")

	// Test getting node for path that doesn't exist at all
	nonExistentNode := tree.GetNode("nonexistent/path")
	// Should return root node since no path matches
	assert.Equal(t, "/", nonExistentNode.path, "Should return root for non-existent paths")

	// Test getting root node
	rootNode := tree.GetNode("")
	assert.Equal(t, "/", rootNode.path, "Empty path should return root node")
}

func TestGetNodeWithTerminalNodes(t *testing.T) {
	// Test GetNode behavior with terminal nodes
	// This ensures terminal flag affects node traversal correctly
	tree := NewTree()

	// Add a terminal node
	terminalRuleset := aclspec.NewRuleSet(
		"terminal",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := tree.AddRuleSet(terminalRuleset)
	assert.NoError(t, err)

	// Verify the terminal node was added
	terminalNode := tree.GetNode("terminal")
	assert.NotNil(t, terminalNode, "Terminal node should exist")
	assert.True(t, terminalNode.IsTerminal(), "Node should be marked as terminal")

	// Test that attempting to add a child under a terminal node should fail
	// This is the correct behavior - terminal nodes should prevent child rulesets
	childRuleset := aclspec.NewRuleSet(
		"terminal/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	err = tree.AddRuleSet(childRuleset)
	// Terminal nodes should prevent child rulesets from being added
	assert.Error(t, err, "Should reject child rulesets under terminal nodes")
	assert.Contains(t, err.Error(), "terminal node", "Error should mention terminal restriction")

	// Test that GetNode stops at terminal node during traversal
	// Since child ruleset was rejected, any path beyond terminal should stop at terminal
	node := tree.GetNode("terminal/child/deeper")
	assert.Equal(t, "terminal", node.path, "Terminal node should stop further traversal")
	
	// Test that GetNearestNodeWithRules also respects terminal nodes
	nearestNode := tree.GetNearestNodeWithRules("terminal/child/file.txt")
	assert.NotNil(t, nearestNode, "Should find the terminal node")
	assert.Equal(t, "terminal", nearestNode.path, "Should stop at terminal node for rule lookup")
	
	// Verify that the child node was not actually created
	childNode := tree.GetNode("terminal/child")
	assert.Equal(t, "terminal", childNode.path, "Child node should not exist, should return terminal parent")
}

func TestTerminalNodeValidation(t *testing.T) {
	// Test that terminal nodes properly prevent child rulesets from being added
	// This explicitly tests the terminal enforcement behavior
	tree := NewTree()

	// Add a terminal node
	terminalRuleset := aclspec.NewRuleSet(
		"secure",
		aclspec.SetTerminal,
		aclspec.NewRule("**", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := tree.AddRuleSet(terminalRuleset)
	assert.NoError(t, err, "Should be able to add terminal node")

	// Verify it's terminal
	node := tree.GetNode("secure")
	assert.True(t, node.IsTerminal(), "Node should be marked as terminal")

	// Try to add direct child - should fail
	childRuleset := aclspec.NewRuleSet(
		"secure/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	err = tree.AddRuleSet(childRuleset)
	assert.Error(t, err, "Should not be able to add child under terminal node")
	assert.Contains(t, err.Error(), "terminal node", "Error should mention terminal node")

	// Try to add deeper nested child - should also fail
	deepChildRuleset := aclspec.NewRuleSet(
		"secure/child/grandchild",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	err = tree.AddRuleSet(deepChildRuleset)
	assert.Error(t, err, "Should not be able to add nested child under terminal node")
	assert.Contains(t, err.Error(), "terminal node", "Error should mention terminal node")

	// Verify that non-terminal nodes still allow children
	nonTerminalRuleset := aclspec.NewRuleSet(
		"open",
		aclspec.UnsetTerminal,
		aclspec.NewRule("**", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	err = tree.AddRuleSet(nonTerminalRuleset)
	assert.NoError(t, err, "Should be able to add non-terminal node")

	// Add child under non-terminal - should succeed
	openChildRuleset := aclspec.NewRuleSet(
		"open/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err = tree.AddRuleSet(openChildRuleset)
	assert.NoError(t, err, "Should be able to add child under non-terminal node")
}

func TestConflictingRuleSetsAtSameLevel(t *testing.T) {
	// Test what happens when adding multiple rulesets to the same path
	// This tests ruleset replacement/overwriting behavior
	tree := NewTree()

	// Add initial ruleset
	initialRuleset := aclspec.NewRuleSet(
		"shared",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := tree.AddRuleSet(initialRuleset)
	assert.NoError(t, err, "Should be able to add initial ruleset")

	// Verify initial ruleset
	node := tree.GetNode("shared")
	assert.NotNil(t, node, "Node should exist")
	assert.False(t, node.IsTerminal(), "Node should not be terminal initially")
	assert.Len(t, node.Rules(), 1, "Should have 1 rule initially")

	// Add conflicting ruleset at the SAME path with different rules and terminal flag
	conflictingRuleset := aclspec.NewRuleSet(
		"shared", // Same path!
		aclspec.SetTerminal, // Different terminal flag
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()), // Different rule
		aclspec.NewRule("**", aclspec.SharedReadAccess("admin@example.com"), aclspec.DefaultLimits()), // Additional rule
	)

	err = tree.AddRuleSet(conflictingRuleset)
	assert.NoError(t, err, "Should be able to add conflicting ruleset (overwrites)")

	// Verify the conflicting ruleset completely replaced the original
	node = tree.GetNode("shared")
	assert.NotNil(t, node, "Node should still exist")
	assert.True(t, node.IsTerminal(), "Node should now be terminal (overwritten)")
	assert.Len(t, node.Rules(), 2, "Should have 2 rules from new ruleset")

	// Verify the conflicting ruleset completely replaced the original
	// The original *.txt rule with PrivateAccess is gone
	// Now we have *.md rule with PublicReadAccess and ** rule with SharedReadAccess
	
	// Test that *.txt files now match the ** rule (not the original *.txt rule)
	rule, err := node.FindBestRule("shared/test.txt")
	assert.NoError(t, err, "Should find rule for *.txt files")
	assert.Equal(t, "**", rule.rule.Pattern, "Should match the ** rule, not original *.txt rule")
	assert.True(t, rule.rule.Access.Read.Contains("admin@example.com"), "Should have admin access (from ** rule)")
	assert.False(t, rule.rule.Access.Read.Contains("*"), "Should NOT have public access (original rule is gone)")

	// Test that *.md files match the more specific *.md rule
	rule, err = node.FindBestRule("shared/test.md")
	assert.NoError(t, err, "Should find rule for *.md files")
	assert.Equal(t, "*.md", rule.rule.Pattern, "Should match the specific *.md rule")
	assert.True(t, rule.rule.Access.Read.Contains("*"), "Should have public read access from *.md rule")

	// Test that other files match the ** rule
	rule, err = node.FindBestRule("shared/anything.xyz")
	assert.NoError(t, err, "Should find rule for other files")
	assert.Equal(t, "**", rule.rule.Pattern, "Should match the ** rule")
	assert.True(t, rule.rule.Access.Read.Contains("admin@example.com"), "Should have admin access from ** rule")

	// Test that the terminal flag now prevents children
	childRuleset := aclspec.NewRuleSet(
		"shared/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.go", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err = tree.AddRuleSet(childRuleset)
	assert.Error(t, err, "Should not be able to add child under terminal node")
	assert.Contains(t, err.Error(), "terminal node", "Error should mention terminal restriction")
}

func TestAddRuleSetErrorCases(t *testing.T) {
	// Test AddRuleSet with various error conditions
	// This improves coverage of edge cases and error handling
	tree := NewTree()

	// Test with nil ruleset
	err := tree.AddRuleSet(nil)
	assert.Error(t, err, "Should reject nil ruleset")
	assert.Contains(t, err.Error(), "ruleset is nil", "Error should indicate nil ruleset")

	// Test with empty ruleset (no rules)
	emptyRuleset := &aclspec.RuleSet{
		Path:     "test",
		Terminal: false,
		Rules:    []*aclspec.Rule{},
	}
	err = tree.AddRuleSet(emptyRuleset)
	assert.Error(t, err, "Should reject empty ruleset")
	assert.Contains(t, err.Error(), "ruleset is empty", "Error should indicate empty ruleset")

	// Test with extremely deep path (path depth > 255)
	deepPath := ""
	for i := 0; i < 300; i++ { // Create path with > 255 components
		deepPath += "a/"
	}
	deepRuleset := aclspec.NewRuleSet(
		deepPath,
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)
	err = tree.AddRuleSet(deepRuleset)
	assert.Error(t, err, "Should reject paths that are too deep")
	assert.Contains(t, err.Error(), "maximum depth exceeded", "Error should indicate depth limit")
}

func TestAddRuleSetPathNormalization(t *testing.T) {
	// Test that AddRuleSet properly normalizes different path formats
	// This ensures consistent path handling across different input formats
	tree := NewTree()

	// Test with path that has leading/trailing separators
	rule := aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits())
	
	// These should all result in the same normalized path
	testPaths := []string{
		"test/path",
		"/test/path",
		"test/path/",
		"/test/path/",
		"./test/path",
	}

	for i, path := range testPaths {
		ruleset := aclspec.NewRuleSet(path, false, rule)
		err := tree.AddRuleSet(ruleset)
		assert.NoError(t, err, "Should accept path format: %s", path)
		
		// All paths should result in the same node being found
		node := tree.GetNode("test/path")
		assert.NotNil(t, node, "Should find node for normalized path (test %d)", i)
		assert.Equal(t, "test/path", node.path, "Path should be normalized consistently (test %d)", i)
	}
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

	// Remove parent ruleset - should only remove parent, not affect child
	removed := tree.RemoveRuleSet("parent")
	assert.True(t, removed)

	// Verify parent node structure still exists but rules are gone
	parentNode, ok := tree.root.GetChild("parent")
	assert.True(t, ok, "Parent node should still exist")
	assert.NotNil(t, parentNode, "Parent node should not be nil")
	assert.Nil(t, parentNode.Rules(), "Parent node should have no rules after removal")

	// Verify child is still there with its rules intact
	childNode, ok := parentNode.GetChild("child")
	assert.True(t, ok, "Child node should still exist")
	assert.NotNil(t, childNode, "Child node should not be nil")
	assert.NotNil(t, childNode.Rules(), "Child node should still have its rules")
	assert.True(t, childNode.IsTerminal(), "Child should still be terminal")

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
	parentNode, ok = tree.root.GetChild("parent")
	assert.True(t, ok)
	assert.NotNil(t, parentNode)

	// Verify child was removed
	_, ok = parentNode.GetChild("child")
	assert.False(t, ok)
}
