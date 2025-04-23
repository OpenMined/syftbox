package acl

import (
	"errors"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
)

var (
	ErrAdminRequired      = errors.New("admin access required")
	ErrWriteRequired      = errors.New("write access required")
	ErrReadRequired       = errors.New("read access required")
	ErrDirsNotAllowed     = errors.New("directories not allowed")
	ErrSymlinksNotAllowed = errors.New("symlinks not allowed")
	ErrFileSizeExceeded   = errors.New("file size exceeds limits")
	ErrInvalidAccessLevel = errors.New("invalid access level")
)

// Rule represents an access control rule for a file or directory in an ACL Node.
// It contains the full pattern of the rule, the rule itself, and the node it applies to
type Rule struct {
	fullPattern string        // full pattern = full path + glob
	rule        *aclspec.Rule // the rule itself
	node        *Node         // the node this rule applies to
}

// CheckAccess checks if the user has permission to perform the specified action on the node.
func (r *Rule) CheckAccess(user *User, level AccessLevel) error {
	// the rule is owned by the user, so they can do anything
	if user.IsOwner {
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
	case AccessWriteACL:
		if !isAdmin {
			return ErrAdminRequired
		}
		return nil
	case AccessWrite:
		if !isWriter {
			return ErrWriteRequired
		}
		return nil
	case AccessRead:
		if !isReader {
			return ErrReadRequired
		}
		return nil
	default:
		return ErrInvalidAccessLevel
	}
}

// CheckLimits checks if the file is within the limits specified by the rule.
func (r *Rule) CheckLimits(info *File) error {
	limits := r.rule.Limits

	if limits == nil {
		return nil
	}

	if limits.MaxFileSize > 0 && info.Size > limits.MaxFileSize {
		return ErrFileSizeExceeded
	}

	if !limits.AllowDirs && (info.IsDir || strings.Count(info.Path, pathSep) > 0) {
		return ErrDirsNotAllowed
	}

	if !limits.AllowSymlinks && info.IsSymlink {
		return ErrSymlinksNotAllowed
	}

	return nil
}
