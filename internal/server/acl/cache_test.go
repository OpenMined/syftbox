package acl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewACLCache(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)
	assert.NotNil(t, cache.index)
	assert.Equal(t, 0, cache.Count())
}

func TestACLCacheGetSet(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test user and request
	user := &User{ID: "testuser"}
	req := NewRequest("test/path", user, AccessRead)

	// Test getting from empty cache
	canAccess, found := cache.Get(req)
	assert.False(t, found)
	assert.False(t, canAccess)

	// Set access permission
	cache.Set(req, true)

	// Get the permission back
	canAccess, found = cache.Get(req)
	assert.True(t, found)
	assert.True(t, canAccess)

	// Test getting with different user
	differentUser := &User{ID: "differentuser"}
	differentReq := NewRequest("test/path", differentUser, AccessRead)
	canAccess, found = cache.Get(differentReq)
	assert.False(t, found)
	assert.False(t, canAccess)
}

func TestACLCacheDelete(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test users and requests
	user1 := &User{ID: "user1"}
	user2 := &User{ID: "user2"}
	req1 := NewRequest("test1/path", user1, AccessRead)
	req2 := NewRequest("test2/path", user2, AccessRead)

	// Set multiple permissions
	cache.Set(req1, true)
	cache.Set(req2, true)

	// Verify both permissions exist
	canAccess1, found1 := cache.Get(req1)
	assert.True(t, found1)
	assert.True(t, canAccess1)

	canAccess2, found2 := cache.Get(req2)
	assert.True(t, found2)
	assert.True(t, canAccess2)

	// Delete permissions for test1 path
	deleted := cache.Delete("test1/path")
	assert.Equal(t, 1, deleted)

	// Verify the deleted permission is gone
	canAccess1, found1 = cache.Get(req1)
	assert.False(t, found1)
	assert.False(t, canAccess1)

	// Verify the other permission still exists
	canAccess2, found2 = cache.Get(req2)
	assert.True(t, found2)
	assert.True(t, canAccess2)
}

func TestACLCacheDeletePrefix(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test users and requests for different paths
	user1 := &User{ID: "user1"}
	user2 := &User{ID: "user2"}
	user3 := &User{ID: "user3"}
	user4 := &User{ID: "user4"}

	req1 := NewRequest("parent/file.txt", user1, AccessRead)
	req2 := NewRequest("parent/child/file.md", user2, AccessRead)
	req3 := NewRequest("parent/child/grandchild/file.go", user3, AccessRead)
	req4 := NewRequest("other/file.py", user4, AccessRead)

	// Set all permissions
	cache.Set(req1, true)
	cache.Set(req2, true)
	cache.Set(req3, true)
	cache.Set(req4, true)

	// Verify all permissions exist initially
	canAccess1, found1 := cache.Get(req1)
	assert.True(t, found1)
	assert.True(t, canAccess1)

	canAccess2, found2 := cache.Get(req2)
	assert.True(t, found2)
	assert.True(t, canAccess2)

	canAccess3, found3 := cache.Get(req3)
	assert.True(t, found3)
	assert.True(t, canAccess3)

	canAccess4, found4 := cache.Get(req4)
	assert.True(t, found4)
	assert.True(t, canAccess4)

	// Delete all permissions with "parent" prefix
	deleted := cache.DeletePrefix("parent")
	assert.Equal(t, 3, deleted)

	// Verify parent-related permissions are deleted
	canAccess1, found1 = cache.Get(req1)
	assert.False(t, found1)
	assert.False(t, canAccess1)

	canAccess2, found2 = cache.Get(req2)
	assert.False(t, found2)
	assert.False(t, canAccess2)

	canAccess3, found3 = cache.Get(req3)
	assert.False(t, found3)
	assert.False(t, canAccess3)

	// Verify unrelated permission still exists
	canAccess4, found4 = cache.Get(req4)
	assert.True(t, found4)
	assert.True(t, canAccess4)
}

func TestACLCacheDeletePrefixExactMatch(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test users and requests with non-overlapping prefixes
	user1 := &User{ID: "user1"}
	user2 := &User{ID: "user2"}
	req1 := NewRequest("exact/file.txt", user1, AccessRead)
	req2 := NewRequest("different/file.md", user2, AccessRead)

	// Set permissions
	cache.Set(req1, true)
	cache.Set(req2, true)

	// Verify both permissions exist
	canAccess1, found1 := cache.Get(req1)
	assert.True(t, found1)
	assert.True(t, canAccess1)

	canAccess2, found2 := cache.Get(req2)
	assert.True(t, found2)
	assert.True(t, canAccess2)

	// Delete with exact prefix match
	deleted := cache.DeletePrefix("exact")
	assert.Equal(t, 1, deleted)

	// Verify only the exact match is deleted
	canAccess1, found1 = cache.Get(req1)
	assert.False(t, found1)
	assert.False(t, canAccess1)

	canAccess2, found2 = cache.Get(req2)
	assert.True(t, found2)
	assert.True(t, canAccess2)
}

func TestACLCacheDeletePrefixEmpty(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create a test user and request
	user := &User{ID: "testuser"}
	req := NewRequest("test/file.txt", user, AccessRead)

	// Set the permission
	cache.Set(req, true)

	// Delete with empty prefix (should delete everything)
	deleted := cache.DeletePrefix("")
	assert.Equal(t, 1, deleted)

	// Verify the permission is deleted
	canAccess, found := cache.Get(req)
	assert.False(t, found)
	assert.False(t, canAccess)
}

func TestACLCacheConcurrentAccess(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test user and request
	user := &User{ID: "testuser"}
	req := NewRequest("test/path", user, AccessRead)

	// Test concurrent Set and Get operations
	done := make(chan bool, 2)

	// Goroutine 1: Set operations
	go func() {
		for i := 0; i < 100; i++ {
			cache.Set(req, true)
		}
		done <- true
	}()

	// Goroutine 2: Get operations
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get(req)
		}
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Verify the final state
	canAccess, found := cache.Get(req)
	assert.True(t, found)
	assert.True(t, canAccess)
}

func TestACLCacheDeniedAccess(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test user and request
	user := &User{ID: "testuser"}
	req := NewRequest("test/path", user, AccessRead)

	// Test setting denied access
	cache.Set(req, false)

	// Get the denied access back
	canAccess, found := cache.Get(req)
	assert.True(t, found)
	assert.False(t, canAccess)
}

func TestACLCacheMultipleDeletes(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test user and request
	user := &User{ID: "testuser"}
	req := NewRequest("test/path", user, AccessRead)

	// Set the permission
	cache.Set(req, true)

	// Delete the same path multiple times (should not panic)
	deleted1 := cache.Delete("test/path")
	assert.Equal(t, 1, deleted1)

	deleted2 := cache.Delete("test/path")
	assert.Equal(t, 0, deleted2)

	deleted3 := cache.Delete("test/path")
	assert.Equal(t, 0, deleted3)

	// Verify the permission is gone
	canAccess, found := cache.Get(req)
	assert.False(t, found)
	assert.False(t, canAccess)
}

func TestACLCacheDeletePrefixNonExistent(t *testing.T) {
	cache := NewACLCache()
	require.NotNil(t, cache)

	// Create test user and request
	user := &User{ID: "testuser"}
	req := NewRequest("test/path", user, AccessRead)

	// Set the permission
	cache.Set(req, true)

	// Delete with non-existent prefix
	deleted := cache.DeletePrefix("nonexistent")
	assert.Equal(t, 0, deleted)

	// Verify the permission still exists
	canAccess, found := cache.Get(req)
	assert.True(t, found)
	assert.True(t, canAccess)
}
