package acl

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/openmined/syftbox/internal/aclspec"
)

// ACLVersion is the version of the node.
// overflow will reset it to 0.
type ACLVersion = uint16

// ACLDepth is the depth of the node in the tree.
type ACLDepth = uint8

const (
	ACLMaxDepth   = 1<<8 - 1  // keep this in sync with the type ACLDepth
	ACLMaxVersion = 1<<16 - 1 // keep this in sync with the type ACLVersion
)

// ACLNode represents a node in the ACL tree.
// Each node corresponds to a part of the path and contains rules for that part.
type ACLNode struct {
	mu       sync.RWMutex
	rules    []*ACLRule          // rules for this part of the path. sorted by specificity.
	children map[string]*ACLNode // key is the part of the path
	owner    string              // owner of the node
	path     string              // path is the full path to this Anode
	terminal bool                // true if this node is a terminal node
	depth    ACLDepth            // depth of the node in the tree. 0 is root node
	version  ACLVersion          // version of the node. incremented on every change
}

// NewACLNode creates a new ACLNode.
func NewACLNode(path string, owner string, terminal bool, depth ACLDepth) *ACLNode {
	// note rules & children are not initialized here.
	// this is to avoid unnecessary allocations, until the node is set with rules.
	return &ACLNode{
		path:     path,
		owner:    owner,
		terminal: terminal,
		depth:    depth,
		version:  0,
	}
}

// GetChild returns the child for the node.
func (n *ACLNode) GetChild(key string) (*ACLNode, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	child, exists := n.children[key]
	return child, exists
}

// SetChild sets the child for the node.
func (n *ACLNode) SetChild(key string, child *ACLNode) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.children == nil {
		n.children = make(map[string]*ACLNode)
	}
	if child == nil {
		delete(n.children, key)
	} else {
		n.children[key] = child
	}
	n.version++
}

// DeleteChild deletes the child for the node.
func (n *ACLNode) DeleteChild(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.children, key)
	n.version++
}

// GetRules returns the rules for the node.
func (n *ACLNode) GetRules() []*ACLRule {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.rules
}

// SetRules the rules, terminal flag and depth for the node.
// Increments the version counter for repeated operation.
func (n *ACLNode) SetRules(rules []*aclspec.Rule, terminal bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if len(rules) > 0 {
		// pre-sort the rules by specificity
		sorted := sortRulesBySpecificity(rules)

		// convert the rules to aclRules
		aclRules := make([]*ACLRule, 0, len(sorted))
		for _, rule := range sorted {
			aclRules = append(aclRules, &ACLRule{
				rule:        rule,
				node:        n,
				fullPattern: filepath.Join(n.path, rule.Pattern),
			})
		}
		n.rules = aclRules
	}

	// set the rules and terminal flag
	n.terminal = terminal

	// increment the version
	n.version++
}

// ClearRules clears the rules for the node.
func (n *ACLNode) ClearRules() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.rules = nil
	n.version++
}

// FindBestRule finds the best matching rule for the given path.
func (n *ACLNode) FindBestRule(path string) (*ACLRule, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.rules == nil {
		return nil, ErrNoRuleFound
	}

	// find the best matching rule
	for _, aclRule := range n.rules {
		if ok, _ := doublestar.Match(aclRule.fullPattern, path); ok {
			return aclRule, nil
		}
	}

	return nil, ErrNoRuleFound
}

// GetOwner returns the owner of the node.
func (n *ACLNode) GetOwner() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.owner
}

// GetVersion returns the version of the node.
func (n *ACLNode) GetVersion() ACLVersion {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.version
}

// GetTerminal returns true if the node is a terminal node.
func (n *ACLNode) GetTerminal() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.terminal
}

// GetDepth returns the depth of the node.
func (n *ACLNode) GetDepth() ACLDepth {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.depth
}

// Equal checks if the node is equal to another node.
func (n *ACLNode) Equal(other *ACLNode) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.path == other.path && n.terminal == other.terminal && n.depth == other.depth && n.version == other.version
}

func calculateGlobSpecificity(glob string) int {
	// early return for the most specific glob patterns
	switch glob {
	case "**":
		return -100
	case "**/*":
		return -99
	}

	// base score = 2L + 10D - wildcard penalty
	score := len(glob)*2 + strings.Count(glob, PathSep)*10

	// penalize base score for substr wildcards
	for i, c := range glob {
		switch c {
		case '*':
			if i == 0 {
				score -= 20 // Leading wildcards are very unspecific
			} else {
				score -= 10 // Other wildcards are less penalized
			}
		case '?', '!', '[', '{':
			score -= 2 // Non * wildcards get smaller penalty
		}
	}

	return score
}

func sortRulesBySpecificity(rules []*aclspec.Rule) []*aclspec.Rule {
	// copy the rules
	clone := slices.Clone(rules)

	// sort by specificity (or priority), descending
	sort.Slice(clone, func(i, j int) bool {
		return calculateGlobSpecificity(clone[i].Pattern) > calculateGlobSpecificity(clone[j].Pattern)
	})

	return clone
}
