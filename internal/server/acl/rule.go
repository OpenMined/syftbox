package acl

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"text/template"

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
	fullPattern string             // full pattern = full path + glob
	rule        *aclspec.Rule      // the rule itself
	node        *ACLNode           // the node this rule applies to
	tplPattern  *template.Template // nil if fullPattern has no template
}

// HasTemplate returns true if this rule contains template variables
func (r *ACLRule) HasTemplate() bool {
	return r.tplPattern != nil
}

// hasUserToken returns true if this rule contains USER tokens in access lists
func (r *ACLRule) hasUserToken() bool {
	return r.rule.Access.Admin.Contains(aclspec.TokenUser) ||
		r.rule.Access.Write.Contains(aclspec.TokenUser) ||
		r.rule.Access.Read.Contains(aclspec.TokenUser)
}

// NewACLRule creates a new ACLRule with template compilation
func NewACLRule(rule *aclspec.Rule, node *ACLNode) *ACLRule {
	fullPattern := ACLJoinPath(node.path, rule.Pattern)

	// Compile template if the pattern contains template variables
	var tplPattern *template.Template
	if HasTemplatePattern(fullPattern) {
		tmpl, err := NewTemplatePattern(fullPattern)
		if err != nil {
			slog.Error("failed to compile template pattern", "error", err)
			return nil
		} else {
			tplPattern = tmpl
		}
	}

	return &ACLRule{
		fullPattern: fullPattern,
		rule:        rule,
		node:        node,
		tplPattern:  tplPattern,
	}
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

// MatchesPath checks if this rule matches the given path for the given user
func (r *ACLRule) MatchesPath(path string, ctx *TemplateContext) (bool, error) {
	pattern := r.fullPattern
	if r.tplPattern != nil {
		var buf strings.Builder
		if err := r.tplPattern.Execute(&buf, ctx); err != nil {
			return false, fmt.Errorf("failed to execute template: %w", err)
		}
		pattern = buf.String()
	}
	return doublestar.Match(pattern, path)
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
	return fmt.Sprintf("ACLRule{fullPattern: %s, tplPattern: %v, rule: %v, node: %v}", r.fullPattern, r.tplPattern, r.rule, r.node)
}

func (r *ACLRule) Clone() *ACLRule {
	return &ACLRule{
		fullPattern: r.fullPattern,
		tplPattern:  r.tplPattern,
		rule:        r.rule,
		node:        r.node,
	}
}
