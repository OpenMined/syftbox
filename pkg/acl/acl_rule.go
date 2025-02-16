package acl

import (
	"path/filepath"
	"strings"
)

type aclRule struct {
	rule        *Rule
	node        *aclNode // back reference to the node
	fullPattern string
}

// CanAccess checks if the user has permission to perform the specified action on the node.
func (r *aclRule) CanAccess(user string, action Action) bool {
	// the rule is owned by the user, so they can do anything
	if IsOwner(r.node.path, user) {
		return true
	}

	// Capture "*" checks once
	everyoneAdmin := r.rule.Access.Admin.Contains(Everyone)
	everyoneWrite := r.rule.Access.Write.Contains(Everyone)
	everyoneRead := r.rule.Access.Read.Contains(Everyone)

	// Only check user-specific permissions if "*" is false
	isAdmin := everyoneAdmin || r.rule.Access.Admin.Contains(user)
	isWriter := isAdmin || everyoneWrite || r.rule.Access.Write.Contains(user)
	isReader := isWriter || everyoneRead || r.rule.Access.Read.Contains(user)

	// Use a switch with fallthrough for permission hierarchy
	switch action {
	case ActionFileWriteACL, ActionFileReadACL:
		return isAdmin
	case ActionFileWrite:
		return isWriter
	case ActionFileRead:
		return isReader
	default:
		return false
	}
}

// WithinLimts checks if the file is within the limits specified by the rule.
func (r *aclRule) WithinLimts(info *FileInfo) bool {
	limits := r.rule.Limits

	if limits == nil {
		return true
	}

	if info.Size > limits.MaxFileSize {
		return false
	}

	if !limits.AllowDirs && (info.IsDir || strings.Count(info.Path, string(filepath.Separator)) > 0) {
		return false
	}

	if !limits.AllowSymlinks && info.IsSymlink {
		return false
	}

	return true
}
