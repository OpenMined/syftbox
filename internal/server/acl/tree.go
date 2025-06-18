package acl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
)

var (
	ErrInvalidRuleset   = errors.New("invalid ruleset")
	ErrMaxDepthExceeded = errors.New("maximum depth exceeded")
	ErrNoRuleSet        = errors.New("no ruleset found")
	ErrNoRule           = errors.New("no rules available")
)

const (
	rootPath = "/"
	noOwner  = ""
)

// ACLTree stores the ACL rules in a n-ary tree for efficient lookups.
type ACLTree struct {
	root *ACLNode
}

// NewACLTree creates a new ACLTree.
func NewACLTree() *ACLTree {
	return &ACLTree{
		root: NewACLNode(rootPath, noOwner, false, 0),
	}
}

// Add or update a ruleset in the tree.
func (t *ACLTree) AddRuleSet(ruleset *aclspec.RuleSet) (*ACLNode, error) {
	// Validate the ruleset
	if ruleset == nil {
		return nil, fmt.Errorf("%w: ruleset is nil", ErrInvalidRuleset)
	}

	allRules := ruleset.AllRules()
	if len(allRules) == 0 {
		return nil, fmt.Errorf("%w: ruleset is empty", ErrInvalidRuleset)
	}

	// Clean and split the path
	cleanPath := ACLNormPath(ruleset.Path)
	parts := strings.Split(cleanPath, ACLPathSep)
	pathDepth := strings.Count(cleanPath, ACLPathSep)

	// owner is assumed to be the first part of the path.
	// but in future we can always bake it as a part of the acl schema
	owner := parts[0]
	if owner == "" {
		return nil, fmt.Errorf("%w: owner is empty", ErrInvalidRuleset)
	}

	// Check path depth limit (u8)
	if pathDepth > ACLMaxDepth {
		return nil, ErrMaxDepthExceeded
	}

	// Start at the root node
	current := t.root
	currentDepth := 0

	// Traverse/create the path
	for i, part := range parts {
		currentDepth = i + 1 // Calculate depth based on current position

		// Important: We still process terminal nodes to ensure all ACLs are known to the tree
		// Get or create child node
		child, exists := current.GetChild(part)
		if !exists {
			fullPath := ACLJoinPath(parts[:currentDepth]...)
			child = NewACLNode(fullPath, owner, false, ACLDepth(currentDepth))
			current.SetChild(part, child)
		}

		current = child
	}

	// Set the rules on the final node
	current.SetRules(allRules, ruleset.Terminal)

	return current, nil
}

// GetEffectiveRule returns the most specific rule applicable to the given path.
func (t *ACLTree) GetEffectiveRule(path string) (*ACLRule, error) {
	normalizedPath := ACLNormPath(path)

	node := t.LookupNearestNode(normalizedPath) // O(depth)
	if node == nil {
		return nil, ErrNoRuleSet
	}

	rule, err := node.FindBestRule(normalizedPath) // O(rules|node)
	if err != nil {
		return nil, err // returns ErrNoRuleFound if no rule is found
	}

	return rule, nil
}

// LookupNearestNode returns the nearest node in the tree that has associated rules for the given path.
// It returns nil if no such node is found.
func (t *ACLTree) LookupNearestNode(normalizedPath string) *ACLNode {
	parts := ACLPathSegments(normalizedPath)

	var candidate *ACLNode
	current := t.root

	// candidate only if the root node has rules
	if current.GetRules() != nil {
		candidate = current
	}

	for _, part := range parts {
		// Stop if the current node is terminal.
		if current.GetTerminal() {
			break
		}

		child, exists := current.GetChild(part)
		if !exists {
			break
		}

		current = child
		if child.GetRules() != nil {
			candidate = current
		}
	}

	return candidate
}

// GetNode finds the exact node applicable for the given path.
func (t *ACLTree) GetNode(path string) *ACLNode {
	normalizedPath := ACLNormPath(path)
	parts := ACLPathSegments(normalizedPath)
	current := t.root

	for _, part := range parts {
		if current.GetTerminal() {
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
func (t *ACLTree) RemoveRuleSet(path string) bool {
	var parent *ACLNode
	var lastPart string

	parts := ACLPathSegments(path)
	currentNode := t.root

	for _, part := range parts {
		child, exists := currentNode.GetChild(part)
		if !exists {
			return false
		}

		parent = currentNode
		currentNode = child
		lastPart = part
	}

	// Need to lock parent since we're modifying its children
	parent.DeleteChild(lastPart)

	return true
}
