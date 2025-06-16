package acl

import (
	"fmt"
	"sync"
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
)

func TestNewRuleCache(t *testing.T) {
	// Test creating a new cache
	// This validates the constructor initializes the cache correctly
	cache := NewRuleCache()

	assert.NotNil(t, cache, "NewRuleCache should return non-nil cache")
	assert.NotNil(t, cache.index, "Cache should have initialized index map")
	assert.Empty(t, cache.index, "New cache should start empty")
}

func TestRuleCacheBasicOperations(t *testing.T) {
	// Test basic cache operations: Set, Get, Delete
	// This validates the core cache functionality
	cache := NewRuleCache()
	
	// Create a mock rule for testing
	mockNode := NewNode("test", false, 1)
	mockRule := &Rule{
		fullPattern: "test/*.txt",
		rule:        aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        mockNode,
	}
	
	// Test Get on empty cache
	result := cache.Get("test/file.txt")
	assert.Nil(t, result, "Get should return nil for non-existent entries")
	
	// Test Set and Get
	cache.Set("test/file.txt", mockRule)
	result = cache.Get("test/file.txt")
	assert.Equal(t, mockRule, result, "Get should return the cached rule")
	
	// Test Delete
	cache.Delete("test/file.txt")
	result = cache.Get("test/file.txt")
	assert.Nil(t, result, "Get should return nil after deletion")
}

func TestRuleCacheVersionValidation(t *testing.T) {
	// Test that cache validates rule versions to detect stale entries
	// This is critical for cache invalidation when rules are updated
	cache := NewRuleCache()
	
	// Create a node and rule
	mockNode := NewNode("test", false, 1)
	mockRule := &Rule{
		fullPattern: "test/*.txt",
		rule:        aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        mockNode,
	}
	
	// Cache the rule with current node version
	cache.Set("test/file.txt", mockRule)
	
	// Verify we can retrieve it
	result := cache.Get("test/file.txt")
	assert.Equal(t, mockRule, result, "Should retrieve cached rule with valid version")
	
	// Simulate node version change (like when rules are updated)
	mockNode.SetRules([]*aclspec.Rule{
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	}, false)
	
	// Now the cached entry should be invalid due to version mismatch
	result = cache.Get("test/file.txt")
	assert.Nil(t, result, "Should return nil for stale cache entry with wrong version")
	
	// Verify the stale entry was automatically removed
	assert.NotContains(t, cache.index, "test/file.txt", "Stale entry should be removed from cache")
}

func TestRuleCacheDeletePrefix(t *testing.T) {
	// Test the DeletePrefix operation which removes multiple entries
	// This is important for bulk cache invalidation when directory rules change
	cache := NewRuleCache()
	
	// Create multiple cache entries with related paths
	mockNode := NewNode("test", false, 1)
	mockRule := &Rule{
		fullPattern: "test/*.txt",
		rule:        aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        mockNode,
	}
	
	// Add entries with common prefix
	cache.Set("project/src/file1.go", mockRule)
	cache.Set("project/src/file2.go", mockRule)
	cache.Set("project/docs/readme.md", mockRule)
	cache.Set("project/tests/test1.go", mockRule)
	cache.Set("other/file.txt", mockRule) // Different prefix
	
	// Verify all entries exist
	assert.NotNil(t, cache.Get("project/src/file1.go"))
	assert.NotNil(t, cache.Get("project/src/file2.go"))
	assert.NotNil(t, cache.Get("project/docs/readme.md"))
	assert.NotNil(t, cache.Get("project/tests/test1.go"))
	assert.NotNil(t, cache.Get("other/file.txt"))
	
	// Delete entries with "project/src" prefix
	cache.DeletePrefix("project/src")
	
	// Verify only the prefixed entries were removed
	assert.Nil(t, cache.Get("project/src/file1.go"), "Should remove project/src/file1.go")
	assert.Nil(t, cache.Get("project/src/file2.go"), "Should remove project/src/file2.go")
	assert.NotNil(t, cache.Get("project/docs/readme.md"), "Should keep project/docs/readme.md")
	assert.NotNil(t, cache.Get("project/tests/test1.go"), "Should keep project/tests/test1.go")
	assert.NotNil(t, cache.Get("other/file.txt"), "Should keep other/file.txt")
	
	// Delete broader prefix
	cache.DeletePrefix("project")
	
	// Verify all project entries are removed
	assert.Nil(t, cache.Get("project/docs/readme.md"), "Should remove project/docs/readme.md")
	assert.Nil(t, cache.Get("project/tests/test1.go"), "Should remove project/tests/test1.go")
	assert.NotNil(t, cache.Get("other/file.txt"), "Should keep other/file.txt")
}

func TestRuleCacheDeletePrefixEdgeCases(t *testing.T) {
	// Test DeletePrefix with edge cases and boundary conditions
	// This ensures robust handling of unusual prefix patterns
	cache := NewRuleCache()
	
	mockNode := NewNode("test", false, 1)
	mockRule := &Rule{
		fullPattern: "test/*.txt",
		rule:        aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        mockNode,
	}
	
	// Add test entries
	cache.Set("", mockRule)           // Empty path
	cache.Set("a", mockRule)          // Single character
	cache.Set("ab", mockRule)         // Two characters
	cache.Set("abc", mockRule)        // Three characters
	cache.Set("abd", mockRule)        // Similar prefix
	
	// Test deleting with empty prefix (should remove all entries that start with empty string, which is all entries)
	cache.DeletePrefix("")
	assert.Nil(t, cache.Get(""), "Should remove empty path entry")
	assert.Nil(t, cache.Get("a"), "Should remove single character entry (empty prefix matches all)")
	
	// Re-add entries for next test
	cache.Set("a", mockRule)
	cache.Set("ab", mockRule)
	cache.Set("abc", mockRule)
	cache.Set("abd", mockRule)
	
	// Test deleting with single character prefix
	cache.DeletePrefix("a")
	assert.Nil(t, cache.Get("a"), "Should remove 'a'")
	assert.Nil(t, cache.Get("ab"), "Should remove 'ab'")
	assert.Nil(t, cache.Get("abc"), "Should remove 'abc'")
	assert.Nil(t, cache.Get("abd"), "Should remove 'abd'")
	
	// Test deleting non-existent prefix (should be safe)
	cache.DeletePrefix("nonexistent")
	// Should not crash or cause issues
}

func TestRuleCacheConcurrency(t *testing.T) {
	// Test that cache operations are thread-safe
	// This validates the mutex protection works correctly
	cache := NewRuleCache()
	
	mockNode := NewNode("test", false, 1)
	mockRule := &Rule{
		fullPattern: "test/*.txt",
		rule:        aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        mockNode,
	}
	
	const numGoroutines = 10
	const numOperations = 100
	
	// Test concurrent Set operations
	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("test/%d/%d.txt", id, j)
				cache.Set(key, mockRule)
			}
		}(i)
	}
	wg.Wait()
	
	// Test concurrent Get operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("test/%d/%d.txt", id, j)
				result := cache.Get(key)
				assert.NotNil(t, result, "Should find cached entry")
			}
		}(i)
	}
	wg.Wait()
	
	// Test concurrent Delete operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("test/%d/%d.txt", id, j)
				cache.Delete(key)
			}
		}(i)
	}
	wg.Wait()
	
	// Verify all entries were deleted
	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < numOperations; j++ {
			key := fmt.Sprintf("test/%d/%d.txt", i, j)
			result := cache.Get(key)
			assert.Nil(t, result, "Entry should be deleted")
		}
	}
}

func TestRuleCacheMixedConcurrentOperations(t *testing.T) {
	// Test mixed concurrent operations (Set, Get, Delete, DeletePrefix)
	// This validates thread safety under realistic usage patterns
	cache := NewRuleCache()
	
	mockNode := NewNode("test", false, 1)
	mockRule := &Rule{
		fullPattern: "test/*.txt",
		rule:        aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        mockNode,
	}
	
	const numWorkers = 5
	const duration = 100 // Number of operations per worker
	
	var wg sync.WaitGroup
	
	// Worker 1: Continuous Set operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < duration; i++ {
			key := fmt.Sprintf("set/worker/%d.txt", i)
			cache.Set(key, mockRule)
		}
	}()
	
	// Worker 2: Continuous Get operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < duration; i++ {
			key := fmt.Sprintf("set/worker/%d.txt", i%10) // Access recently set items
			cache.Get(key)
		}
	}()
	
	// Worker 3: Continuous Delete operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < duration; i++ {
			key := fmt.Sprintf("set/worker/%d.txt", i)
			cache.Delete(key)
		}
	}()
	
	// Worker 4: Periodic DeletePrefix operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < duration/10; i++ {
			prefix := fmt.Sprintf("set/worker")
			cache.DeletePrefix(prefix)
		}
	}()
	
	// Worker 5: Mixed operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < duration; i++ {
			key := fmt.Sprintf("mixed/%d.txt", i)
			cache.Set(key, mockRule)
			cache.Get(key)
			if i%5 == 0 {
				cache.Delete(key)
			}
		}
	}()
	
	wg.Wait()
	
	// Test should complete without deadlocks or race conditions
	// The exact final state is unpredictable due to concurrency,
	// but the operations should all complete successfully
}

func TestRuleCacheMemoryManagement(t *testing.T) {
	// Test that cache doesn't leak memory with repeated operations
	// This validates proper cleanup of cache entries
	cache := NewRuleCache()
	
	mockNode := NewNode("test", false, 1)
	mockRule := &Rule{
		fullPattern: "test/*.txt",
		rule:        aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		node:        mockNode,
	}
	
	// Add many entries
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("test/%d.txt", i)
		cache.Set(key, mockRule)
	}
	
	// Verify entries exist
	assert.Equal(t, 1000, len(cache.index), "Should have 1000 entries")
	
	// Clear using DeletePrefix
	cache.DeletePrefix("test")
	
	// Verify all entries are removed
	assert.Equal(t, 0, len(cache.index), "Should have 0 entries after DeletePrefix")
	
	// Add entries again to test reuse
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("reuse/%d.txt", i)
		cache.Set(key, mockRule)
	}
	
	assert.Equal(t, 100, len(cache.index), "Should be able to reuse cache after clearing")
}