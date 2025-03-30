package acl

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/yashgorana/syftbox-go/internal/aclspec"
)

// Node represents a node in the ACL tree.
// Each node corresponds to a part of the path and contains rules for that part.
type Node struct {
	mu       sync.RWMutex
	rules    []*Rule // rules for this part of the path. key is rule.Pattern
	path     string
	children map[string]*Node
	terminal bool
	depth    uint8
	version  uint8
}

func NewNode(path string, terminal bool, depth uint8) *Node {
	return &Node{
		path:     path,
		terminal: terminal,
		depth:    depth,
	}
}

// Set the rules, terminal flag and depth for the node.
// Increments the version counter for repeated operation.
func (n *Node) Set(rules []*aclspec.Rule, terminal bool, depth uint8) {
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
				fullPattern: filepath.Join(n.path, rule.Pattern),
			})
		}
		n.rules = aclRules
	}

	// set the rules and terminal flag
	n.terminal = terminal

	// set the depth and version
	n.depth = depth

	// increment the version. uint8 overflow will reset it to 0.
	n.version++
}

// FindBestRule finds the best matching rule for the given path.
func (n *Node) FindBestRule(path string) (*Rule, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.rules == nil {
		return nil, fmt.Errorf("no rules found for path %s", path)
	}

	// find the best matching rule
	for _, aclRule := range n.rules {
		ok, _ := doublestar.Match(aclRule.fullPattern, path)
		if ok {
			return aclRule, nil
		}
	}

	return nil, fmt.Errorf("no matching rule found for path %s", path)
}

// Equal checks if the node is equal to another node.
func (n *Node) Equal(other *Node) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.path == other.path && n.terminal == other.terminal && n.depth == other.depth
}

func (n *Node) Version() uint8 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.version
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
	score := len(glob)*2 + strings.Count(glob, pathSep)*10

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
