package acl

import (
	"strings"
	"sync"
)

type cacheEntry struct {
	rule    *aclRule
	version pCounter
}

type aclRuleCache struct {
	index map[string]*cacheEntry
	mu    sync.RWMutex
}

func newAclRuleCache() *aclRuleCache {
	return &aclRuleCache{
		index: make(map[string]*cacheEntry),
	}
}

func (c *aclRuleCache) Get(path string) *aclRule {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.index[path]
	if !ok {
		return nil
	}

	// validate the cache entry
	valid := cached.rule.node.Version() == cached.version
	if !valid {
		// if the version is not valid, remove the entry from the cache
		c.DeletePrefix(path)
		return nil
	}

	return cached.rule
}

func (c *aclRuleCache) Set(path string, rule *aclRule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.index[path] = &cacheEntry{
		rule:    rule,
		version: rule.node.Version(),
	}
}

func (c *aclRuleCache) DeletePrefix(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// iterate over index keys and delete the entry
	for k := range c.index {
		if strings.HasPrefix(k, path) {
			delete(c.index, k)
		}
	}
}
