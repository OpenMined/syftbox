package acl

import (
	"strings"
	"sync"
)

type aclRuleCache struct {
	index map[string]*aclRule
	mu    sync.RWMutex
}

func (c *aclRuleCache) Get(path string) *aclRule {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.index[path]
	if !ok {
		return nil
	}
	return cached
}

func (c *aclRuleCache) Set(path string, entry *aclRule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.index[path] = entry
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

func newAclRuleCache() *aclRuleCache {
	return &aclRuleCache{index: make(map[string]*aclRule)}
}
