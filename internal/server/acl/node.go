package acl

import (
	"fmt"
	"sync"

	"github.com/openmined/syftbox/internal/aclspec"
)

// ACLVersion is the version of the node.
// overflow will reset it to 0.
type ACLVersion = uint16

// ACLDepth is the depth of the node in the tree.
type ACLDepth = uint8

const (
	ACLMaxDepth   = 1<<8 - 1
	ACLMaxVersion = 1<<16 - 1
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

// GetChildCount returns the number of children for the node.
func (n *ACLNode) GetChildCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.children)
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
			aclRules = append(aclRules, NewACLRule(rule, n))
		}
		n.rules = aclRules
	} else {
		// Clear rules if empty
		n.rules = nil
	}

	// set the terminal flag
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

func (n *ACLNode) String() string {
	return fmt.Sprintf("ACLNode{path: %s, terminal: %v, depth: %v, version: %v}", n.path, n.terminal, n.depth, n.version)
}
