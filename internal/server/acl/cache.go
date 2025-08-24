package acl

import (
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

const (
	aclCacheTTL        = time.Hour * 1
	aclAccessCacheSize = 100_000
)

type aclCacheKey string

// this has combinatorial explosion potential...
// but it is required to keep things fast for static/templated/user-specific rules.
// the TTL & max size should keep it in check + selective sync will reduce the number of keys
func newCacheKeyByUser(req *ACLRequest) aclCacheKey {
	return aclCacheKey(fmt.Sprintf("%s:%s:%d", req.Path, req.User.ID, req.Level))
}

// ACLCache stores the access level for a given path and user.
type ACLCache struct {
	index *expirable.LRU[aclCacheKey, bool]
}

// NewACLCache creates a new ACLCache.
func NewACLCache() *ACLCache {
	return &ACLCache{
		index: expirable.NewLRU[aclCacheKey, bool](aclAccessCacheSize, nil, aclCacheTTL), // 0 = LRU off
	}
}

// Get returns the access level for the given path and user.
func (c *ACLCache) Get(req *ACLRequest) (bool, bool) {
	key := newCacheKeyByUser(req)
	return c.index.Get(key)
}

// Set sets the access level for the given path and user.
func (c *ACLCache) Set(req *ACLRequest, canAccess bool) {
	key := newCacheKeyByUser(req)
	c.index.Add(key, canAccess)
}

// Delete deletes the access level for the given path and user.
func (c *ACLCache) Delete(path string) int {
	return c.DeletePrefix(path)
}

// DeletePrefix deletes the access level for all paths that match the given prefix.
func (c *ACLCache) DeletePrefix(path string) int {
	deleted := 0

	// iterate over index keys and delete the entry
	keys := c.index.Keys()
	for _, k := range keys {
		if strings.HasPrefix(string(k), path) {
			c.index.Remove(k)
			deleted++
		}
	}

	return deleted
}

func (c *ACLCache) Count() int {
	return c.index.Len()
}

// Clear removes all entries from the cache
func (c *ACLCache) Clear() {
	c.index.Purge()
}
