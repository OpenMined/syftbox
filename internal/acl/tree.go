package acl

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yashgorana/syftbox-go/internal/aclspec"
)

// Tree stores the ACL rules in a tree structure for efficient lookups.
type Tree struct {
	root *Node
}

func NewTree() *Tree {
	return &Tree{
		root: NewNode(pathSep, false, 0),
	}
}

// AddRuleSet adds or updates a set of rules to the tree.
func (t *Tree) AddRuleSet(ruleset *aclspec.RuleSet) error {
	allRules := ruleset.AllRules()
	if ruleset == nil || len(allRules) == 0 {
		return fmt.Errorf("ruleset must have at least one rule")
	}

	parts := strings.Split(filepath.Clean(ruleset.Path), pathSep)
	pathDepth := strings.Count(ruleset.Path, pathSep)

	// depth is u8
	if pathDepth > 255 {
		return fmt.Errorf("path exceeds maximum depth of 255")
	}

	current := t.root
	depth := current.depth

	for _, part := range parts {
		if part == "" {
			continue
		}
		depth++

		// * DO NOT return if current.terminal. Let the tree know all the ACLs
		// * else we'll have to re-build the whole tree again when someone flips terminal flag

		current.mu.Lock()
		if current.children == nil {
			current.children = make(map[string]*Node)
		}

		child, exists := current.children[part]
		if !exists {
			fullPath := strings.Join(parts[:depth], pathSep)
			child = NewNode(fullPath, false, depth)
			current.children[part] = child
		}
		current.mu.Unlock()

		current = child
	}

	current.Set(allRules, ruleset.Terminal, depth)
	return nil
}

// FindNearestNodeWithRules finds the most specific node with rules applicable to the given path.
func (t *Tree) FindNearestNodeWithRules(path string) (*Node, error) {
	parts := strings.Split(filepath.Clean(path), pathSep)

	current := t.root
	lastNodeWithRules := current

	for _, part := range parts {
		if part == "" {
			continue
		}

		if current.rules != nil {
			lastNodeWithRules = current
		}

		if current.terminal {
			lastNodeWithRules = current
			break
		}

		// Lock only the current node while checking its children
		current.mu.RLock()
		child, exists := current.children[part]
		current.mu.RUnlock()

		if !exists {
			break
		}
		current = child
	}

	// Final rules check outside the loop
	if lastNodeWithRules.rules == nil {
		return nil, fmt.Errorf("no rules found for path %s", path)
	}

	return lastNodeWithRules, nil
}

// GetNearestNode finds the most specific node applicable to the given path.
func (t *Tree) GetNearestNode(path string) *Node {
	parts := strings.Split(filepath.Clean(path), pathSep)
	current := t.root

	for _, part := range parts {
		if part == "" {
			continue
		}

		if current.terminal {
			return current
		}

		current.mu.RLock()
		child, exists := current.children[part]
		current.mu.RUnlock()

		if !exists {
			return current
		}
		current = child
	}

	return current
}

// RemoveRuleSet removes a ruleset at the specified path.
func (t *Tree) RemoveRuleSet(path string) bool {
	var parent *Node
	var lastPart string

	parts := strings.Split(filepath.Clean(path), pathSep)
	current := t.root

	for _, part := range parts {
		if part == "" {
			continue
		}

		current.mu.RLock()
		child, exists := current.children[part]
		current.mu.RUnlock()

		if !exists {
			return false
		}

		parent = current
		current = child
		lastPart = part
	}

	// Need to lock parent since we're modifying its children
	parent.mu.Lock()
	defer parent.mu.Unlock()

	delete(parent.children, lastPart)
	return true
}
