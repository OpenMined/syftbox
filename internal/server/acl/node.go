package acl

import (
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/openmined/syftbox/internal/aclspec"
)

// Node represents a node in the ACL tree.
// Each node corresponds to a part of the path and contains rules for that part.
type Node struct {
	mu       sync.RWMutex
	rules    []*Rule          // rules for this part of the path. sorted by specificity.
	path     string           // path is the full path to this node
	children map[string]*Node // key is the part of the path
	terminal bool             // true if this node is a terminal node
	depth    uint8            // depth of the node in the tree. 0 is root node
	version  uint8            // version of the node. incremented on every change
}

func NewNode(path string, terminal bool, depth uint8) *Node {
	return &Node{
		path:     path,
		terminal: terminal,
		depth:    depth,
	}
}

func (n *Node) Version() uint8 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.version
}

func (n *Node) IsTerminal() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.terminal
}

func (n *Node) Depth() uint8 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.depth
}

func (n *Node) Rules() []*Rule {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.rules
}

func (n *Node) SetChild(key string, child *Node) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.children == nil {
		n.children = make(map[string]*Node)
	}
	if child == nil {
		delete(n.children, key)
	} else {
		n.children[key] = child
	}
}

func (n *Node) GetChild(key string) (*Node, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	child, exists := n.children[key]
	return child, exists
}

func (n *Node) DeleteChild(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.children, key)
}

// SetRules the rules, terminal flag and depth for the node.
// Increments the version counter for repeated operation.
func (n *Node) SetRules(rules []*aclspec.Rule, terminal bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if len(rules) > 0 {
		// pre-sort the rules by specificity
		sorted := sortBySpecificity(rules)

		// convert the rules to aclRules
		aclRules := make([]*Rule, 0, len(sorted))
		for _, rule := range sorted {
			aclRules = append(aclRules, &Rule{
				rule:        rule,
				node:        n,
				fullPattern: ACLJoinPath(n.path, rule.Pattern),
			})
		}
		n.rules = aclRules
	} else {
		// Clear rules when empty or nil slice is provided
		n.rules = nil
	}

	// set the rules and terminal flag
	n.terminal = terminal

	// increment the version. uint8 overflow will reset it to 0.
	n.version++
}

// FindBestRule finds the best matching rule for the given path.
func (n *Node) FindBestRule(path string) (*Rule, error) {
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

// Equal checks if the node is equal to another node.
func (n *Node) Equal(other *Node) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.path == other.path && n.terminal == other.terminal && n.depth == other.depth
}

func globSpecificityScore(glob string) int {
	// exact
	switch glob {
	case "**":
		return -100
	case "**/*":
		return -99
	}

	// 2L + 10D - wildcard penalty
	// Use forward slash for glob patterns
	score := len(glob)*2 + strings.Count(glob, ACLPathSep)*10

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

func sortBySpecificity(rules []*aclspec.Rule) []*aclspec.Rule {
	// copy the rules
	clone := append([]*aclspec.Rule(nil), rules...)

	// sort by specificity, descending
	sort.Slice(clone, func(i, j int) bool {
		return globSpecificityScore(clone[i].Pattern) > globSpecificityScore(clone[j].Pattern)
	})
	return clone
}
