package acl

import (
	"strings"

	"github.com/yashgorana/syftbox-go/internal/aclspec"
)

// Rule represents an access control rule for a file or directory in an ACL Node.
// It contains the full pattern of the rule, the rule itself, and the node it applies to.
type Rule struct {
	fullPattern string
	rule        *aclspec.Rule
	node        *Node
}

// CheckAccess checks if the user has permission to perform the specified action on the node.
func (r *Rule) CheckAccess(user *User, level AccessLevel) bool {
	// the rule is owned by the user, so they can do anything
	if user.IsOwner {
		return true
	}

	// Capture "*" checks once
	everyoneAdmin := r.rule.Access.Admin.Contains(aclspec.Everyone)
	everyoneWrite := r.rule.Access.Write.Contains(aclspec.Everyone)
	everyoneRead := r.rule.Access.Read.Contains(aclspec.Everyone)

	// Only check user-specific permissions if "*" is false
	isAdmin := everyoneAdmin || r.rule.Access.Admin.Contains(user.ID)
	isWriter := isAdmin || everyoneWrite || r.rule.Access.Write.Contains(user.ID)
	isReader := isWriter || everyoneRead || r.rule.Access.Read.Contains(user.ID)

	// Use a switch with fallthrough for permission hierarchy
	switch level {
	case AccessWriteACL, AccessReadACL:
		return isAdmin
	case AccessWrite:
		return isWriter
	case AccessRead:
		return isReader
	default:
		return false
	}
}

// CheckLimits checks if the file is within the limits specified by the rule.
func (r *Rule) CheckLimits(info *File) bool {
	limits := r.rule.Limits

	if limits == nil {
		return true
	}

	if limits.MaxFileSize > 0 && info.Size > limits.MaxFileSize {
		return false
	}

	if !limits.AllowDirs && (info.IsDir || strings.Count(info.Path, pathSep) > 0) {
		return false
	}

	if !limits.AllowSymlinks && info.IsSymlink {
		return false
	}

	return true
}
