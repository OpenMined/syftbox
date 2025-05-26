package acl

import (
	"errors"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
)

var (
	ErrNoAdminAccess      = errors.New("no admin access")
	ErrNoWriteAccess      = errors.New("no write access")
	ErrNoReadAccess       = errors.New("no read access")
	ErrDirsNotAllowed     = errors.New("directories not allowed")
	ErrSymlinksNotAllowed = errors.New("symlinks not allowed")
	ErrFileSizeExceeded   = errors.New("file size exceeds limits")
	ErrInvalidAccessLevel = errors.New("invalid access level")
)

// ACLRule represents an access control rule for a file or directory in an ACL Node.
// It contains the full pattern of the rule, the rule itself, and the node it applies to
type ACLRule struct {
	fullPattern string        // full pattern = full path + glob
	rule        *aclspec.Rule // the rule itself
	node        *ACLNode      // the node this rule applies to
}

// Owner returns the owner of the rule (inherited from the node)
func (r *ACLRule) Owner() string {
	return r.node.GetOwner()
}

// Version returns the version of the rule (inherited from the node)s
func (r *ACLRule) Version() ACLVersion {
	return r.node.GetVersion()
}

// CheckAccess checks if the user has permission to perform the specified action on the node.
func (r *ACLRule) CheckAccess(user *User, level AccessLevel) error {
	// the rule is owned by the user, so they can do anything
	if r.Owner() == user.ID {
		return nil
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
	case AccessAdmin:
		if !isAdmin {
			return ErrNoAdminAccess
		}
		return nil
	case AccessWrite:
		if !isWriter {
			return ErrNoWriteAccess
		}
		return nil
	case AccessRead:
		if !isReader {
			return ErrNoReadAccess
		}
		return nil
	default:
		return ErrInvalidAccessLevel
	}
}

// CheckLimits checks if the file is within the limits specified by the rule.
func (r *ACLRule) CheckLimits(info *File) error {
	limits := r.rule.Limits

	if limits == nil {
		return nil
	}

	if limits.MaxFileSize > 0 && info.Size > limits.MaxFileSize {
		return ErrFileSizeExceeded
	}

	if !limits.AllowDirs && (info.IsDir || strings.Count(info.Path, PathSep) > 0) {
		return ErrDirsNotAllowed
	}

	if !limits.AllowSymlinks && info.IsSymlink {
		return ErrSymlinksNotAllowed
	}

	return nil
}
