package acl

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
)

var (
	ErrInvalidRuleset   = errors.New("invalid ruleset")
	ErrMaxDepthExceeded = errors.New("maximum depth exceeded")
	ErrNoRuleFound      = errors.New("no rule found")
)

// Tree stores the ACL rules in a n-ary tree for efficient lookups.
type Tree struct {
	root *Node
}

func NewTree() *Tree {
	return &Tree{
		root: NewNode(pathSep, false, 0),
	}
}

// Add or update a ruleset in the tree.
func (t *Tree) AddRuleSet(ruleset *aclspec.RuleSet) error {
	// Validate the ruleset
	if ruleset == nil {
		return fmt.Errorf("%w: ruleset is nil", ErrInvalidRuleset)
	}

	allRules := ruleset.AllRules()
	if len(allRules) == 0 {
		return fmt.Errorf("%w: ruleset is empty", ErrInvalidRuleset)
	}

	// Clean and split the path
	cleanPath := stripSep(ruleset.Path)
	parts := strings.Split(cleanPath, pathSep)
	pathDepth := strings.Count(cleanPath, pathSep)

	// Check path depth limit (u8)
	if pathDepth > 255 {
		return ErrMaxDepthExceeded
	}

	// Start at the root node
	current := t.root
	currentDepth := current.depth

	// Traverse/create the path
	for _, part := range parts {
		currentDepth++

		// Important: We still process terminal nodes to ensure all ACLs are known to the tree
		// Get or create child node
		child, exists := current.GetChild(part)
		if !exists {
			fullPath := strings.Join(parts[:currentDepth], pathSep)
			child = NewNode(fullPath, false, currentDepth)
			current.SetChild(part, child)
		}
		current = child
	}

	// Set the rules on the final node
	current.SetRules(allRules, ruleset.Terminal)

	return nil
}

// Get rule for the given path
func (t *Tree) GetRule(path string) (*Rule, error) {

	node := t.GetNearestNodeWithRules(path) // O(depth)
	if node == nil {
		return nil, ErrNoRuleFound
	}

	rule, err := node.FindBestRule(path) // O(rules|node)
	if err != nil {
		return nil, err
	}

	return rule, nil
}

// GetNearestNodeWithRules returns the nearest node in the tree that has associated rules for the given path.
// It returns nil if no such node is found.
func (t *Tree) GetNearestNodeWithRules(path string) *Node {
	parts := pathParts(path)

	var candidate *Node
	current := t.root

	for _, part := range parts {
		// Stop if the current node is terminal.
		if current.IsTerminal() {
			break
		}

		child, exists := current.GetChild(part)
		if !exists {
			break
		}

		current = child
		if child.Rules() != nil {
			candidate = current
		}
	}

	return candidate
}

// GetNode finds the exact node applicable for the given path.
func (t *Tree) GetNode(path string) *Node {
	parts := pathParts(path)
	current := t.root

	for _, part := range parts {
		if current.IsTerminal() {
			break
		}

		child, exists := current.GetChild(part)
		if !exists {
			break
		}
		current = child
	}

	return current
}

// Removes a ruleset at the specified path
func (t *Tree) RemoveRuleSet(path string) bool {
	var parent *Node
	var lastPart string

	parts := pathParts(path)
	current := t.root

	for _, part := range parts {
		child, exists := current.GetChild(part)
		if !exists {
			return false
		}

		parent = current
		current = child
		lastPart = part
	}

	// Need to lock parent since we're modifying its children
	parent.DeleteChild(lastPart)

	return true
}

func pathParts(path string) []string {
	return strings.Split(stripSep(path), pathSep)
}

func stripSep(path string) string {
	return strings.TrimLeft(filepath.Clean(path), pathSep)
}
