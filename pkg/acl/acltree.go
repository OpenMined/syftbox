package acl

import (
	"path/filepath"
	"strings"
)

const Terminal = true

type aclTree struct {
	root *aclNode
}

func newAclTree() *aclTree {
	return &aclTree{
		root: newAclNode(string(filepath.Separator), false, 0),
	}
}

func (t *aclTree) AddRuleSet(path string, ruleset *RuleSet) error {
	if ruleset == nil || len(ruleset.Rules) == 0 {
		return nil
	}

	path = filepath.Clean(path)
	parts := strings.Split(path, string(filepath.Separator))
	current := t.root
	depth := current.depth

	for _, part := range parts {
		if part == "" {
			continue
		}
		depth++

		// note - we don't want to do this
		// because if we do we'll have to re-build the whole tree again when someone flips terminal flag
		// let perm tree know the whole picture and then it can decide what to do based on this flag
		// if current.terminal {
		// 	return nil
		// }

		current.mu.Lock()
		if current.children == nil {
			current.children = make(map[string]*aclNode)
		}
		child, exists := current.children[part]
		if !exists {
			fullpath := strings.Join(parts[:depth], string(filepath.Separator))
			child = newAclNode(fullpath, false, depth)
			current.children[part] = child
		}
		current.mu.Unlock()
		current = child
	}

	current.Set(ruleset.Rules, ruleset.Terminal, depth)

	return nil
}

func (t *aclTree) FindNode(path string) *aclNode {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	current := t.root

	for _, part := range parts {
		if part == "" {
			continue
		}
		if current.terminal {
			return current
		}
		current.mu.RLock()
		if _, exists := current.children[part]; !exists {
			current.mu.RUnlock()
			return current
		}
		current.mu.RUnlock()
		current = current.children[part]
	}

	return current
}

func (t *aclTree) RemoveRuleSet(path string) bool {
	path = filepath.Clean(path)
	parts := strings.Split(path, string(filepath.Separator))
	current := t.root
	parent := t.root

	for _, part := range parts {
		if part == "" {
			continue
		}

		current.mu.RLock()
		if _, exists := current.children[part]; !exists {
			current.mu.RUnlock()
			return false
		}
		current.mu.RUnlock()
		parent = current
		current = current.children[part]
	}

	current.mu.Lock()
	defer current.mu.Unlock()

	delete(parent.children, parts[len(parts)-1])

	return true
}
