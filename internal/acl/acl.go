package acl

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yashgorana/syftbox-go/internal/aclspec"
)

var pathSep = string(filepath.Separator)

// AclService helps to manage and enforce access control rules for file system operations.
type AclService struct {
	tree  *Tree
	cache *RuleCache
}

// NewAclService creates a new ACL service instance
func NewAclService() *AclService {
	return &AclService{
		tree:  NewTree(),
		cache: NewRuleCache(),
	}
}

func (s *AclService) LoadRuleSets(ruleSets []*aclspec.RuleSet) error {
	for _, ruleSet := range ruleSets {
		if err := s.tree.AddRuleSet(ruleSet); err != nil {
			return err
		}
	}
	return nil
}

// AddRuleSet adds a new set of rules to the service.
func (s *AclService) AddRuleSet(ruleSet *aclspec.RuleSet) error {
	return s.tree.AddRuleSet(ruleSet)
}

// RemoveRuleSet removes a ruleset at the specified path.
// Returns true if a ruleset was removed, false otherwise.
func (s *AclService) RemoveRuleSet(path string) bool {
	s.cache.DeletePrefix(path)
	return s.tree.RemoveRuleSet(path)
}

// GetNearestRule finds the most specific rule applicable to the given path.
// Returns nil if no rule is found.
func (s *AclService) GetNearestRule(path string) (*Rule, error) {
	path = strings.TrimLeft(filepath.Clean(path), pathSep)

	// cache hit
	cachedRule := s.cache.Get(path) // O(1)
	if cachedRule != nil {
		return cachedRule, nil
	}

	// cache miss
	node, err := s.tree.FindNearestNodeWithRules(path) // O(depth)
	if err != nil {
		return nil, fmt.Errorf("failed to find node with rules: %w", err)
	}

	rule, err := node.FindBestRule(path) // O(rules|node)
	if err != nil {
		return nil, err
	}

	// cache the result
	s.cache.Set(path, rule) // O(1)

	return rule, nil
}

// CanAccess checks if a user has the specified access permission for a file.
func (s *AclService) CanAccess(user *User, file *File, level AccessLevel) (bool, error) {
	if user.IsOwner {
		return true, nil
	}

	filePath := strings.TrimLeft(filepath.Clean(file.Path), pathSep)
	rule, err := s.GetNearestRule(filePath)
	if err != nil {
		return false, err
	} else if rule == nil {
		return false, fmt.Errorf("no rule found for path %s", filePath)
	}

	fileLimits := true
	isAcl := aclspec.IsAclFile(filePath)

	// elevate action for ACL files
	if isAcl && level == AccessRead {
		level = AccessReadACL
	} else if isAcl && level == AccessWrite {
		level = AccessWriteACL
	} else if level == AccessWrite {
		// writes need to be checked against the file limits
		fileLimits = rule.CheckLimits(file)
	}

	return rule.CheckAccess(user, level) && fileLimits, nil
}

// String returns a string representation of the ACL service's rule tree.
func (s *AclService) String() string {
	return s.tree.String()
}
