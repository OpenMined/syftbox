package acl

import (
	"strings"
	"sync"
)

// ACLCache stores the effective ACL rule for a given path.
type ACLCache struct {
	index map[string]*ACLRule // Normalized ACLPath -> ACLRule
	mu    sync.RWMutex
}

// NewACLCache creates a new ACLCache.
func NewACLCache() *ACLCache {
	return &ACLCache{
		index: make(map[string]*ACLRule),
	}
}

// Get returns the effective ACL rule for the given path.
func (c *ACLCache) Get(path string) *ACLRule {
	c.mu.RLock()
	cacheRule, ok := c.index[path]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	return cacheRule
}

// Set sets the effective ACL rule for the given path.
func (c *ACLCache) Set(path string, rule *ACLRule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.index[path] = rule
}

// Delete deletes the effective ACL rule for the given path.
func (c *ACLCache) Delete(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.index, path)
}

// DeletePrefix deletes the effective ACL rule for all paths that match the given prefix.
func (c *ACLCache) DeletePrefix(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	deleted := 0

	// iterate over index keys and delete the entry
	for k := range c.index {
		if strings.HasPrefix(k, path) {
			delete(c.index, k)
			deleted++
		}
	}

	return deleted
}

func (c *ACLCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.index)
}
