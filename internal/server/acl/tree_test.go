package acl

import (
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
)

func TestNewACLTree(t *testing.T) {
	tree := NewACLTree()
	assert.NotNil(t, tree)
	assert.NotNil(t, tree.root)

	// The ACL system uses forward slashes internally on all platforms for glob compatibility.
	// We explicitly test for "/" rather than pathSep (which is "\" on Windows) because
	// the ACL system is a platform-independent abstraction layer.
	assert.Equal(t, "/", tree.root.path)

	assert.Empty(t, tree.root.children)
	assert.Nil(t, tree.root.GetRules())
}

func TestAddRuleSet(t *testing.T) {
	tree := NewACLTree()

	ruleset := aclspec.NewRuleSet(
		"test/path",
		aclspec.UnsetTerminal,
		aclspec.NewDefaultRule(aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	node, err := tree.AddRuleSet(ruleset)

	// check root node "/"
	assert.NoError(t, err)
	assert.NotNil(t, node)
	assert.Nil(t, tree.root.GetRules())
	assert.Equal(t, "/", tree.root.path)
	assert.Equal(t, ACLDepth(0), tree.root.GetDepth())

	// check node "test"
	child, ok := tree.root.GetChild("test")
	assert.True(t, ok)
	assert.NotNil(t, child)
	assert.Nil(t, child.GetRules())
	assert.Equal(t, "test", child.path)
	assert.Equal(t, ACLDepth(1), child.GetDepth())

	// check node "path"
	child, ok = child.GetChild("path")
	assert.True(t, ok)
	assert.NotNil(t, child)
	assert.Equal(t, "test/path", child.path)
	assert.Equal(t, ACLDepth(2), child.GetDepth())
	assert.NotNil(t, child.GetRules())
}

func TestTreeTraversal(t *testing.T) {
	tree := NewACLTree()

	// Add rulesets with nested paths
	ruleset1 := aclspec.NewRuleSet(
		"parent",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"parent/child",
		aclspec.UnsetTerminal, // Non-terminal to allow grandchild
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ruleset3 := aclspec.NewRuleSet(
		"parent/child/grandchild",
		aclspec.SetTerminal,
		aclspec.NewRule("*.go", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	_, err := tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	_, err = tree.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	_, err = tree.AddRuleSet(ruleset3)
	assert.NoError(t, err)

	// Test finding nearest node with rules for different paths
	node := tree.LookupNearestNode("parent/file.txt")
	assert.Equal(t, "parent", node.path)

	node = tree.LookupNearestNode("parent/child/document.md")
	assert.Equal(t, "parent/child", node.path)

	node = tree.LookupNearestNode("parent/child/grandchild/main.go")
	assert.Equal(t, "parent/child/grandchild", node.path)

	// Test inheritance - terminal nodes (like grandchild) block inheritance from higher levels
	node = tree.LookupNearestNode("parent/child/unknown.txt")
	assert.Equal(t, "parent/child", node.path)

	// Test path that doesn't exist in the tree
	node = tree.LookupNearestNode("unknown/path")
	assert.Nil(t, node)
}

func TestRemoveRuleSet(t *testing.T) {
	tree := NewACLTree()

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

	_, err := tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	_, err = tree.AddRuleSet(ruleset2)
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
	tree := NewACLTree()

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

	_, err := tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	_, err = tree.AddRuleSet(ruleset2)
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
	// Terminal nodes allow children to be added but stop traversal during lookups
	tree := NewACLTree()

	// Add a terminal node with catch-all rule
	terminalRuleset := aclspec.NewRuleSet(
		"terminal",
		aclspec.SetTerminal,
		aclspec.NewRule("**", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	_, err := tree.AddRuleSet(terminalRuleset)
	assert.NoError(t, err)

	// Verify the terminal node was added
	terminalNode := tree.GetNode("terminal")
	assert.NotNil(t, terminalNode, "Terminal node should exist")
	assert.True(t, terminalNode.GetTerminal(), "Node should be marked as terminal")

	// Add a child under the terminal node - this should SUCCEED
	// The tree allows all nodes to be added for performance (avoids tree rebuilds)
	childRuleset := aclspec.NewRuleSet(
		"terminal/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	_, err = tree.AddRuleSet(childRuleset)
	assert.NoError(t, err, "Should allow child rulesets under terminal nodes (tree contains all ACLs)")

	// GetNode should stop at terminal node during lookup traversal
	// Even though child exists in tree, GetNode stops at terminal boundary
	childNode := tree.GetNode("terminal/child")
	assert.Equal(t, "terminal", childNode.path, "GetNode should stop at terminal boundary")

	// When looking for paths beyond terminal, it also stops at the terminal node
	deepNode := tree.GetNode("terminal/child/deeper")
	assert.Equal(t, "terminal", deepNode.path, "Lookup should stop at terminal node")

	// Verify child actually exists in tree structure by accessing parent's children directly
	terminalNode = tree.GetNode("terminal")
	actualChild, exists := terminalNode.GetChild("child")
	assert.True(t, exists, "Child should exist in tree structure")
	assert.Equal(t, "terminal/child", actualChild.path, "Child should have correct path")
	assert.False(t, actualChild.GetTerminal(), "Child should not be terminal")

	// AND: LookupNearestNode should also stop at terminal nodes
	nearestNode := tree.LookupNearestNode("terminal/child/file.txt")
	assert.NotNil(t, nearestNode, "Should find the terminal node")
	assert.Equal(t, "terminal", nearestNode.path, "Rule lookup should stop at terminal node")

	// This means child rules are ignored for inheritance even though they exist in the tree  
	// Test with child path - should get rule from terminal node (** pattern matches everything)
	rule, err := tree.GetEffectiveRule("terminal/child/test.md")
	assert.NoError(t, err, "Should find rule from terminal node")
	assert.Equal(t, "terminal", rule.node.path, "Rule should come from terminal node, not child")
	assert.Equal(t, "**", rule.rule.Pattern, "Should use terminal node's catch-all rule")
}

func TestTerminalNodeValidation(t *testing.T) {
	// Test that terminal nodes control inheritance but allow children to be added
	// This explicitly tests the correct terminal behavior
	tree := NewACLTree()

	// Add a terminal node
	terminalRuleset := aclspec.NewRuleSet(
		"secure",
		aclspec.SetTerminal,
		aclspec.NewRule("**", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	_, err := tree.AddRuleSet(terminalRuleset)
	assert.NoError(t, err, "Should be able to add terminal node")

	// Verify it's terminal
	node := tree.GetNode("secure")
	assert.True(t, node.GetTerminal(), "Node should be marked as terminal")

	// Add direct child - should succeed (tree allows all nodes for performance)
	childRuleset := aclspec.NewRuleSet(
		"secure/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	_, err = tree.AddRuleSet(childRuleset)
	assert.NoError(t, err, "Should be able to add child under terminal node (exists in tree)")

	// Add deeper nested child - should also succeed
	deepChildRuleset := aclspec.NewRuleSet(
		"secure/child/grandchild",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	_, err = tree.AddRuleSet(deepChildRuleset)
	assert.NoError(t, err, "Should be able to add nested child under terminal node")

	// BUT: Terminal nodes should stop traversal for rule lookups
	// All paths under secure/ should resolve to the secure terminal node
	
	// Direct child path should resolve to terminal parent
	nearestNode := tree.LookupNearestNode("secure/child/test.txt")
	assert.NotNil(t, nearestNode, "Should find a node")
	assert.Equal(t, "secure", nearestNode.path, "Should resolve to terminal parent, not child")

	// Deep child path should also resolve to terminal parent
	nearestNode = tree.LookupNearestNode("secure/child/grandchild/test.md")
	assert.NotNil(t, nearestNode, "Should find a node")
	assert.Equal(t, "secure", nearestNode.path, "Should resolve to terminal parent, not grandchild")

	// Rule lookup should use terminal node's rules
	rule, err := tree.GetEffectiveRule("secure/child/test.txt")
	assert.NoError(t, err, "Should find rule for child path")
	assert.Equal(t, "secure", rule.node.path, "Rule should come from terminal parent")
	assert.Equal(t, "**", rule.rule.Pattern, "Should use terminal node's catch-all rule")

	// Non-terminal nodes should work normally
	nonTerminalRuleset := aclspec.NewRuleSet(
		"open",
		aclspec.UnsetTerminal,
		aclspec.NewRule("**", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	_, err = tree.AddRuleSet(nonTerminalRuleset)
	assert.NoError(t, err, "Should be able to add non-terminal node")

	// Add child under non-terminal - should succeed and be accessible
	openChildRuleset := aclspec.NewRuleSet(
		"open/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	_, err = tree.AddRuleSet(openChildRuleset)
	assert.NoError(t, err, "Should be able to add child under non-terminal node")

	// Child under non-terminal should be accessible for rule lookups
	nearestNode = tree.LookupNearestNode("open/child/test.txt")
	assert.NotNil(t, nearestNode, "Should find a node")
	assert.Equal(t, "open/child", nearestNode.path, "Should find the actual child node, not parent")
}

func TestConflictingRuleSetsAtSameLevel(t *testing.T) {
	// Test what happens when adding multiple rulesets to the same path
	// This tests ruleset replacement/overwriting behavior
	tree := NewACLTree()

	// Add initial ruleset
	initialRuleset := aclspec.NewRuleSet(
		"shared",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	_, err := tree.AddRuleSet(initialRuleset)
	assert.NoError(t, err, "Should be able to add initial ruleset")

	// Verify initial ruleset
	node := tree.GetNode("shared")
	assert.NotNil(t, node, "Node should exist")
	assert.False(t, node.GetTerminal(), "Node should not be terminal initially")
	assert.Len(t, node.GetRules(), 1, "Should have 1 rule initially")

	// Add conflicting ruleset at the SAME path with different rules and terminal flag
	conflictingRuleset := aclspec.NewRuleSet(
		"shared", // Same path!
		aclspec.SetTerminal, // Different terminal flag
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()), // Different rule
		aclspec.NewRule("**", aclspec.SharedReadAccess("admin@example.com"), aclspec.DefaultLimits()), // Additional rule
	)

	_, err = tree.AddRuleSet(conflictingRuleset)
	assert.NoError(t, err, "Should be able to add conflicting ruleset (overwrites)")

	// Verify the conflicting ruleset completely replaced the original
	node = tree.GetNode("shared")
	assert.NotNil(t, node, "Node should still exist")
	assert.True(t, node.GetTerminal(), "Node should now be terminal (overwritten)")
	assert.Len(t, node.GetRules(), 2, "Should have 2 rules from new ruleset")

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

	// Test that the terminal flag now controls inheritance
	childRuleset := aclspec.NewRuleSet(
		"shared/child",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.go", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	_, err = tree.AddRuleSet(childRuleset)
	assert.NoError(t, err, "Should be able to add child under terminal node (tree allows all nodes)")

	// But rule lookups should stop at terminal node
	rule, err = tree.GetEffectiveRule("shared/child/test.go")
	assert.NoError(t, err, "Should find rule for child path")
	assert.Equal(t, "shared", rule.node.path, "Rule should come from terminal parent, not child")
	assert.Equal(t, "**", rule.rule.Pattern, "Should use parent's ** rule, not child's *.go rule")
}

func TestAddRuleSetErrorCases(t *testing.T) {
	// Test AddRuleSet with various error conditions
	// This improves coverage of edge cases and error handling
	tree := NewACLTree()

	// Test with nil ruleset
	_, err := tree.AddRuleSet(nil)
	assert.Error(t, err, "Should reject nil ruleset")
	assert.Contains(t, err.Error(), "ruleset is nil", "Error should indicate nil ruleset")

	// Test with empty ruleset (no rules)
	emptyRuleset := &aclspec.RuleSet{
		Path:     "test",
		Terminal: false,
		Rules:    []*aclspec.Rule{},
	}
	_, err = tree.AddRuleSet(emptyRuleset)
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
	_, err = tree.AddRuleSet(deepRuleset)
	assert.Error(t, err, "Should reject paths that are too deep")
	assert.Contains(t, err.Error(), "maximum depth exceeded", "Error should indicate depth limit")
}

func TestAddRuleSetPathNormalization(t *testing.T) {
	// Test that AddRuleSet properly normalizes different path formats
	// This ensures consistent path handling across different input formats
	tree := NewACLTree()

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
		_, err := tree.AddRuleSet(ruleset)
		assert.NoError(t, err, "Should accept path format: %s", path)
		
		// All paths should result in the same node being found
		node := tree.GetNode("test/path")
		assert.NotNil(t, node, "Should find node for normalized path (test %d)", i)
		assert.Equal(t, "test/path", node.path, "Path should be normalized consistently (test %d)", i)
	}
}

func TestNestedRuleSetRemoval(t *testing.T) {
	tree := NewACLTree()

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

	_, err := tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	_, err = tree.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	// Remove parent - with original behavior, this removes the entire subtree
	removed := tree.RemoveRuleSet("parent")
	assert.True(t, removed)

	// Verify both parent and child are gone (original behavior)
	_, ok := tree.root.GetChild("parent")
	assert.False(t, ok, "Parent node should be completely removed")

	// Add the parent ruleset back
	_, err = tree.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	// Add the child ruleset back
	_, err = tree.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	// Remove just the child
	removed = tree.RemoveRuleSet("parent/child")
	assert.True(t, removed)

	// Verify parent still exists but child was removed
	parentNode, ok := tree.root.GetChild("parent")
	assert.True(t, ok, "Parent node should exist after removing just child")
	assert.NotNil(t, parentNode, "Parent node should not be nil")

	// Verify child was removed
	_, ok = parentNode.GetChild("child")
	assert.False(t, ok, "Child node should be removed")
}
