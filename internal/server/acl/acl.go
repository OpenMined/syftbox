package acl

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
)

// ACLService helps to manage and enforce access control rules for file system operations.
type ACLService struct {
	tree  *ACLTree
	cache *ACLCache
}

// NewACLService creates a new ACL service instance
func NewACLService() *ACLService {
	return &ACLService{
		tree:  NewACLTree(),
		cache: NewACLCache(),
	}
}

// AddRuleSet adds or updates a new set of rules to the service.
func (s *ACLService) AddRuleSet(ruleSet *aclspec.RuleSet) (ACLVersion, error) {
	node, err := s.tree.AddRuleSet(ruleSet)
	if err != nil {
		return 0, err
	}

	deleted := s.cache.DeletePrefix(ruleSet.Path)
	slog.Debug("updated rule set", "path", node.path, "version", node.version, "cache.deleted", deleted)
	return node.version, nil
}

// RemoveRuleSet removes a ruleset at the specified path.
// Returns true if a ruleset was removed, false otherwise.
// path must be a dir or dir/syft.pub.yaml
func (s *ACLService) RemoveRuleSet(path string) bool {
	path = aclspec.WithoutACLPath(path)
	if ok := s.tree.RemoveRuleSet(path); ok {
		deleted := s.cache.DeletePrefix(path)
		slog.Debug("deleted cached rules", "path", path, "count", deleted)
		return true
	}
	return false
}

// GetRule finds the most specific rule applicable to the given path.
func (s *ACLService) GetRule(path string) (*ACLRule, error) {
	path = ACLNormPath(path)

	// cache hit
	cachedRule := s.cache.Get(path) // O(1)
	if cachedRule != nil {
		return cachedRule, nil
	}

	// cache miss
	rule, err := s.tree.GetEffectiveRule(path) // O(depth)
	if err != nil {
		return nil, fmt.Errorf("no effective rules for path '%s': %w", path, err)
	}

	// cache the result
	s.cache.Set(path, rule) // O(1)

	return rule, nil
}

// CanAccess checks if a user has the specified access permission for a file.
func (s *ACLService) CanAccess(user *User, file *File, level AccessLevel) error {
	// early return if user is the owner
	if isOwner(file.Path, user.ID) {
		return nil
	}

	// get the effective rule for the file
	rule, err := s.GetRule(file.Path)
	if err != nil {
		return err
	}

	// Elevate ACL file writes to admin level
	if aclspec.IsACLFile(file.Path) && level >= AccessCreate {
		level = AccessAdmin
	}

	// Check file limits for write operations
	if level >= AccessCreate {
		if err := rule.CheckLimits(file); err != nil {
			return fmt.Errorf("file limits exceeded for user '%s' on path '%s': %w", user.ID, file.Path, err)
		}
	}

	// finally check the access
	if err := rule.CheckAccess(user, level); err != nil {
		return fmt.Errorf("access denied for user '%s' on path '%s': %w", user.ID, file.Path, err)
	}

	return nil
}

// String returns a string representation of the ACL service's rule tree.
func (s *ACLService) String() string {
	return s.tree.String()
}

// checks if the user is the owner of the path
func isOwner(path string, user string) bool {
	path = ACLNormPath(path)
	return strings.HasPrefix(path, user)
}
