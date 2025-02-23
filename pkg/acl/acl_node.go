package acl

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
)

type aclNode struct {
	mu       sync.RWMutex
	rules    []*aclRule // rules for this part of the path. key is rule.Pattern
	path     string
	children map[string]*aclNode
	terminal bool
	depth    pCounter
	version  pCounter
}

func newAclNode(path string, terminal bool, depth pCounter) *aclNode {
	return &aclNode{
		path:     path,
		terminal: terminal,
		depth:    depth,
	}
}

// Set the rules, terminal flag and depth for the node.
// Increments the version counter for repeated operation.
func (n *aclNode) Set(rules []*Rule, terminal bool, depth pCounter) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if len(rules) > 0 {
		// pre-sort the rules by specificity
		sorted := sortBySpecificity(rules)

		// convert the rules to aclRules
		aclRules := make([]*aclRule, 0, len(sorted))
		for _, rule := range sorted {
			aclRules = append(aclRules, &aclRule{
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
func (n *aclNode) FindBestRule(path string) (*aclRule, error) {
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
func (n *aclNode) Equal(other *aclNode) bool {
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
	score := len(glob)*2 + strings.Count(glob, PathSep)*10

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

func sortBySpecificity(rules []*Rule) []*Rule {
	// copy the rules
	clone := append([]*Rule(nil), rules...)

	// sort by specificity, descending
	sort.Slice(clone, func(i, j int) bool {
		return globSpecificityScore(clone[i].Pattern) > globSpecificityScore(clone[j].Pattern)
	})
	return clone
}
