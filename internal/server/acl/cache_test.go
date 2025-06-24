package acl

import (
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewACLCache(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)
	assert.NotNil(t, cache.index)
	assert.Empty(t, cache.index)
}

func TestACLCacheGetSet(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Test getting from empty cache
	rule := cache.Get("test/path")
	assert.Nil(t, rule)

	// Create a test rule
	testRule := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("test", "owner", false, 1),
		fullPattern: "test/*.txt",
	}

	// Set the rule
	cache.Set("test/path", testRule)

	// Get the rule back
	retrievedRule := cache.Get("test/path")
	require.NotNil(t, retrievedRule)
	assert.Equal(t, testRule, retrievedRule)

	// Test getting a different path
	differentRule := cache.Get("different/path")
	assert.Nil(t, differentRule)
}

func TestACLCacheDelete(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test rules
	rule1 := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("test1", "owner1", false, 1),
		fullPattern: "test1/*.txt",
	}

	rule2 := &ACLRule{
		rule:        aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("test2", "owner2", false, 1),
		fullPattern: "test2/*.md",
	}

	// Set multiple rules
	cache.Set("test1/path", rule1)
	cache.Set("test2/path", rule2)

	// Verify both rules exist
	retrievedRule1 := cache.Get("test1/path")
	require.NotNil(t, retrievedRule1)
	assert.Equal(t, rule1, retrievedRule1)

	retrievedRule2 := cache.Get("test2/path")
	require.NotNil(t, retrievedRule2)
	assert.Equal(t, rule2, retrievedRule2)

	// Delete one rule
	cache.Delete("test1/path")

	// Verify the deleted rule is gone
	deletedRule := cache.Get("test1/path")
	assert.Nil(t, deletedRule)

	// Verify the other rule still exists
	remainingRule := cache.Get("test2/path")
	require.NotNil(t, remainingRule)
	assert.Equal(t, rule2, remainingRule)
}

func TestACLCacheDeletePrefix(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test rules for different paths
	rule1 := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("parent", "owner1", false, 1),
		fullPattern: "parent/*.txt",
	}

	rule2 := &ACLRule{
		rule:        aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("parent/child", "owner2", false, 2),
		fullPattern: "parent/child/*.md",
	}

	rule3 := &ACLRule{
		rule:        aclspec.NewRule("*.go", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("parent/child/grandchild", "owner3", false, 3),
		fullPattern: "parent/child/grandchild/*.go",
	}

	rule4 := &ACLRule{
		rule:        aclspec.NewRule("*.py", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("other", "owner4", false, 1),
		fullPattern: "other/*.py",
	}

	// Set all rules
	cache.Set("parent/file.txt", rule1)
	cache.Set("parent/child/file.md", rule2)
	cache.Set("parent/child/grandchild/file.go", rule3)
	cache.Set("other/file.py", rule4)

	// Verify all rules exist initially
	assert.NotNil(t, cache.Get("parent/file.txt"))
	assert.NotNil(t, cache.Get("parent/child/file.md"))
	assert.NotNil(t, cache.Get("parent/child/grandchild/file.go"))
	assert.NotNil(t, cache.Get("other/file.py"))

	// Delete all rules with "parent" prefix
	cache.DeletePrefix("parent")

	// Verify parent-related rules are deleted
	assert.Nil(t, cache.Get("parent/file.txt"))
	assert.Nil(t, cache.Get("parent/child/file.md"))
	assert.Nil(t, cache.Get("parent/child/grandchild/file.go"))

	// Verify unrelated rule still exists
	remainingRule := cache.Get("other/file.py")
	require.NotNil(t, remainingRule)
	assert.Equal(t, rule4, remainingRule)
}

func TestACLCacheDeletePrefixExactMatch(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test rules with non-overlapping prefixes
	rule1 := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("exact", "owner1", false, 1),
		fullPattern: "exact/*.txt",
	}

	rule2 := &ACLRule{
		rule:        aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("different", "owner2", false, 1),
		fullPattern: "different/*.md",
	}

	// Set rules
	cache.Set("exact/file.txt", rule1)
	cache.Set("different/file.md", rule2)

	// Verify both rules exist
	assert.NotNil(t, cache.Get("exact/file.txt"))
	assert.NotNil(t, cache.Get("different/file.md"))

	// Delete with exact prefix match
	cache.DeletePrefix("exact")

	// Verify only the exact match is deleted
	assert.Nil(t, cache.Get("exact/file.txt"))
	assert.NotNil(t, cache.Get("different/file.md"))
}

func TestACLCacheDeletePrefixEmpty(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create a test rule
	rule := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("test", "owner", false, 1),
		fullPattern: "test/*.txt",
	}

	// Set the rule
	cache.Set("test/file.txt", rule)

	// Delete with empty prefix (should delete everything)
	cache.DeletePrefix("")

	// Verify the rule is deleted
	assert.Nil(t, cache.Get("test/file.txt"))
}

func TestACLCacheConcurrentAccess(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test rule
	rule := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("test", "owner", false, 1),
		fullPattern: "test/*.txt",
	}

	// Test concurrent Set and Get operations
	done := make(chan bool, 2)

	// Goroutine 1: Set operations
	go func() {
		for i := 0; i < 100; i++ {
			cache.Set("test/path", rule)
		}
		done <- true
	}()

	// Goroutine 2: Get operations
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get("test/path")
		}
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Verify the final state
	retrievedRule := cache.Get("test/path")
	require.NotNil(t, retrievedRule)
	assert.Equal(t, rule, retrievedRule)
}

func TestACLCacheNilRule(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Test setting nil rule
	cache.Set("test/path", nil)

	// Get the nil rule back
	retrievedRule := cache.Get("test/path")
	assert.Nil(t, retrievedRule)
}

func TestACLCacheMultipleDeletes(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test rule
	rule := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("test", "owner", false, 1),
		fullPattern: "test/*.txt",
	}

	// Set the rule
	cache.Set("test/path", rule)

	// Delete the same path multiple times (should not panic)
	cache.Delete("test/path")
	cache.Delete("test/path")
	cache.Delete("test/path")

	// Verify the rule is gone
	assert.Nil(t, cache.Get("test/path"))
}

func TestACLCacheDeletePrefixNonExistent(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test rule
	rule := &ACLRule{
		rule:        aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		node:        NewACLNode("test", "owner", false, 1),
		fullPattern: "test/*.txt",
	}

	// Set the rule
	cache.Set("test/path", rule)

	// Delete with non-existent prefix
	cache.DeletePrefix("nonexistent")

	// Verify the rule still exists
	retrievedRule := cache.Get("test/path")
	require.NotNil(t, retrievedRule)
	assert.Equal(t, rule, retrievedRule)
}
