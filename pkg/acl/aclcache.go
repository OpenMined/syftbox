package acl

import "sync"

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

func newAclRuleCache() *aclRuleCache {
	return &aclRuleCache{index: make(map[string]*aclRule)}
}
