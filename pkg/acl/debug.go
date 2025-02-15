package acl

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func (l *Limit) String() string {
	return fmt.Sprintf("maxFiles:%d maxFileSize:%d allowDirs:%t allowSymlinks:%t", l.MaxFiles, l.MaxFileSize, l.AllowDirs, l.AllowSymlinks)
}

func (r *Rule) String() string {
	return fmt.Sprintf("pattern:%s %s %s", r.Pattern, r.Access, r.Limits)
}

// String implements the Stringer interface for PTree
func (t *aclTree) String() string {
	if t.root == nil {
		return "<empty tree>"
	}
	var sb strings.Builder
	t.root.buildString(&sb, "", true, true)
	return sb.String()
}

// // String implements the Stringer interface for PNode
// func (n *PNode) String() string {
// 	var sb strings.Builder
// 	n.buildString(&sb, "", true, false)
// 	return sb.String()
// }

// buildString recursively builds the string representation of the tree
func (n *aclNode) buildString(sb *strings.Builder, prefix string, isLast bool, isRoot bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !isRoot {
		marker := "└── "
		if !isLast {
			marker = "├── "
		}
		sb.WriteString(prefix)
		sb.WriteString(marker)
	}

	// Write the current node with basic info
	sb.WriteString(filepath.Base(n.path))
	sb.WriteString(fmt.Sprintf(" (d:%d, v:%d", n.depth, n.version))
	if len(n.rules) > 0 {
		sb.WriteString(fmt.Sprintf(", rules:%d", len(n.rules)))
		// sb.WriteString(fmt.Sprintf(", rules:%d, ptr:%p", len(n.rules), n.rules))
	}
	if n.terminal {
		sb.WriteString(", TERMINAL")
	}
	sb.WriteString(")\n")

	// Calculate the new prefix for children
	childPrefix := prefix
	if !isRoot {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	// Print rules as leaves
	// if len(n.rules) > 0 {
	// 	strRules := make([]string, 0, len(n.rules))
	// 	for _, rule := range n.rules {
	// 		strRules = append(strRules, rule.String())
	// 	}
	// 	sort.Strings(strRules)

	// 	for i, strRule := range strRules {
	// 		sb.WriteString(childPrefix)
	// 		if i == len(strRules)-1 && len(n.children) == 0 {
	// 			sb.WriteString("└── ")
	// 		} else {
	// 			sb.WriteString("├── ")
	// 		}
	// 		sb.WriteString(fmt.Sprintf("RULE: %s\n", strRule))
	// 	}
	// }

	// Get and sort children keys
	children := make([]string, 0, len(n.children))
	for k := range n.children {
		children = append(children, k)
	}
	sort.Strings(children)

	// Print children
	for i, key := range children {
		isLastChild := i == len(children)-1
		n.children[key].buildString(sb, childPrefix, isLastChild, false)
	}
}
