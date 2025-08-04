package acl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	mapset "github.com/deckarep/golang-set/v2"
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
// todo decouple this from aclspec.Rule
type ACLRule struct {
	fullPattern string        // full pattern = full path + glob
	rule        *aclspec.Rule // the rule itself
	node        *ACLNode      // the node this rule applies to
	matcher     Matcher       // generic matcher
}

// NewACLRule creates a new ACLRule with template compilation
func NewACLRule(rule *aclspec.Rule, node *ACLNode) (*ACLRule, error) {
	fullPattern := ACLJoinPath(node.path, rule.Pattern)

	// create a generic matcher for the rule
	matcher, err := matcherFromPattern(fullPattern)
	if err != nil {
		return nil, fmt.Errorf("new acl rule: %w", err)
	}

	return &ACLRule{
		fullPattern: fullPattern,
		rule:        rule,
		node:        node,
		matcher:     matcher,
	}, nil
}

// Owner returns the owner of the rule (inherited from the node)
func (r *ACLRule) Owner() string {
	return r.node.GetOwner()
}

// Version returns the version of the rule (inherited from the node)s
func (r *ACLRule) Version() ACLVersion {
	return r.node.GetVersion()
}

func (r *ACLRule) GetLimits() *aclspec.Limits {
	return r.rule.Limits
}

func (r *ACLRule) GetAccess() *aclspec.Access {
	return r.rule.Access
}

// Match checks if this rule matches the given path for the given user
func (r *ACLRule) Match(path string, user *User) (bool, error) {
	return r.matcher.Match(path, user)
}

// CheckAccess checks if the user has permission to perform the specified action on the node.
func (r *ACLRule) CheckAccess(req *ACLRequest) error {
	// the rule is owned by the user, so they can do anything
	if r.Owner() == req.User.ID {
		return nil
	}

	// Resolve USER token in access lists
	adminUsers := r.resolveAccessList(r.rule.Access.Admin, req.User.ID)
	writeUsers := r.resolveAccessList(r.rule.Access.Write, req.User.ID)
	readUsers := r.resolveAccessList(r.rule.Access.Read, req.User.ID)

	// Check permissions hierarchically
	isAdmin := r.hasAccess(adminUsers, req.User.ID)
	isWriter := isAdmin || r.hasAccess(writeUsers, req.User.ID)
	isReader := isWriter || r.hasAccess(readUsers, req.User.ID)

	// Use a switch with fallthrough for permission hierarchy
	switch req.Level {
	case AccessAdmin:
		if !isAdmin {
			return ErrNoAdminAccess
		}
		return nil
	case AccessWrite, AccessCreate:
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
func (r *ACLRule) CheckLimits(req *ACLRequest) error {
	limits := r.GetLimits()

	if limits == nil {
		return nil
	}

	if limits.MaxFileSize > 0 && req.File.Size > limits.MaxFileSize {
		return ErrFileSizeExceeded
	}

	if !limits.AllowDirs && (req.File.IsDir || strings.Count(req.Path, ACLPathSep) > 0) {
		return ErrDirsNotAllowed
	}

	if !limits.AllowSymlinks && req.File.IsSymlink {
		return ErrSymlinksNotAllowed
	}

	return nil
}

func (r *ACLRule) String() string {
	return fmt.Sprintf("ACLRule{fullPattern: %s, matcher: %v, rule: %v, node: %v}", r.fullPattern, r.matcher.Type(), r.rule, r.node)
}

func (r *ACLRule) Clone() *ACLRule {
	return &ACLRule{
		fullPattern: r.fullPattern,
		rule:        r.rule,
		node:        r.node,
		matcher:     r.matcher,
	}
}

// resolveAccessList resolves USER token in an access list
func (r *ACLRule) resolveAccessList(accessList mapset.Set[string], userID string) mapset.Set[string] {
	// if USER token is in the access list, replace it with the actual user ID
	if accessList.Contains(aclspec.TokenUser) {
		clone := mapset.NewSet(accessList.ToSlice()...)
		clone.Add(userID)
		clone.Remove(aclspec.TokenUser)
		return clone
	}

	return accessList
}

// hasAccess checks if a user has access in the given access list
func (r *ACLRule) hasAccess(accessList mapset.Set[string], userID string) bool {
	if accessList.Contains(aclspec.TokenEveryone) {
		return true
	}

	if accessList.Contains(userID) {
		return true
	}

	// iterate and check if ids are glob matches
	for user := range accessList.Iter() {
		// if the user is a glob pattern, check if it matches the user ID
		if strings.ContainsAny(user, "*?[]") {
			if matched, _ := doublestar.Match(user, userID); matched {
				return true
			}
		}
	}

	return false
}

// hasUserToken returns true if this rule contains USER tokens in access lists
func (r *ACLRule) hasUserToken() bool {
	return r.rule.Access.Admin.Contains(aclspec.TokenUser) ||
		r.rule.Access.Write.Contains(aclspec.TokenUser) ||
		r.rule.Access.Read.Contains(aclspec.TokenUser)
}

// Compile creates a user-specific copy of a rule with USER tokens resolved
func (r *ACLRule) Compile(user *User) *ACLRule {
	// if the rule doesn't have a USER token or is not a template, return the original rule
	if r.matcher.Type() != MatcherTypeTemplate || !r.hasUserToken() {
		return r
	}

	clone := r.Clone()
	clone.rule = &aclspec.Rule{
		Pattern: r.rule.Pattern,
		Access: &aclspec.Access{
			Admin: r.resolveAccessList(r.rule.Access.Admin, user.ID),
			Write: r.resolveAccessList(r.rule.Access.Write, user.ID),
			Read:  r.resolveAccessList(r.rule.Access.Read, user.ID),
		},
		Limits: r.rule.Limits,
	}
	return clone
}
