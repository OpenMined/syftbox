package acl

import (
	"strings"
	"sync"
)

type cacheEntry struct {
	rule    *Rule
	version uint8
}

type RuleCache struct {
	index map[string]*cacheEntry
	mu    sync.RWMutex
}

func NewRuleCache() *RuleCache {
	return &RuleCache{
		index: make(map[string]*cacheEntry),
	}
}

func (c *RuleCache) Get(path string) *Rule {
	c.mu.RLock()
	cached, ok := c.index[path]
	c.mu.RUnlock()
	if !ok {
		return nil
	}

	// validate the cache entry
	valid := cached.rule.node.Version() == cached.version
	if !valid {
		c.Delete(path)
		return nil
	}

	return cached.rule
}

func (c *RuleCache) Set(path string, rule *Rule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.index[path] = &cacheEntry{
		rule:    rule,
		version: rule.node.Version(),
	}
}

func (c *RuleCache) Delete(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.index, path)
}

func (c *RuleCache) DeletePrefix(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// iterate over index keys and delete the entry
	for k := range c.index {
		if strings.HasPrefix(k, path) {
			delete(c.index, k)
		}
	}
}
