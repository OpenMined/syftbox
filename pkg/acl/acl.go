// Package acl provides access control list functionality for file system operations.
package acl

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// FileInfo represents metadata about a file including its path, type, size, and hash information.
type FileInfo struct {
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
	ModTime   time.Time
	Crc32     uint32
	Hash      string
}

// Action represents a permission bit flag for different file operations.
type Action uint8

func (a Action) String() string {
	switch a {
	case ActionFileRead:
		return "read"
	case ActionFileWrite:
		return "write"
	case ActionFileReadACL:
		return "read_acl"
	case ActionFileWriteACL:
		return "write_acl"
	default:
		return "unknown"
	}
}

// Action constants define different types of file permissions
const (
	ActionFileRead Action = 1 << iota
	ActionFileWrite
	ActionFileReadACL
	ActionFileWriteACL
)

// AclService helps to manage and enforce access control rules for file system operations.
type AclService struct {
	tree  *aclTree
	cache *aclRuleCache
}

// NewAclService creates a new ACL service instance
func NewAclService() *AclService {
	return &AclService{
		tree:  newAclTree(),
		cache: newAclRuleCache(),
	}
}

func (s *AclService) LoadRuleSets(ruleSets []*RuleSet) error {
	for _, ruleSet := range ruleSets {
		if err := s.tree.AddRuleSet(ruleSet); err != nil {
			return err
		}
	}
	return nil
}

// AddRuleSet adds a new set of rules to the service.
func (s *AclService) AddRuleSet(ruleSet *RuleSet) error {
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
func (s *AclService) GetNearestRule(path string) (*aclRule, error) {
	path = strings.TrimLeft(filepath.Clean(path), PathSep)

	cachedRule := s.cache.Get(path)                    // O(1)
	node, err := s.tree.FindNearestNodeWithRules(path) // O(depth)
	if err != nil {
		return nil, fmt.Errorf("failed to find node with rules: %w", err)
	}

	// validate cache hit
	if cachedRule != nil && node.Equal(cachedRule.node) {
		return cachedRule, nil
	}

	// cache miss
	rule, err := node.FindBestRule(path) // O(rules|node)
	if err != nil {
		return nil, err
	}

	// cache the result
	s.cache.Set(path, rule) // O(1)

	return rule, nil
}

// CanAccess checks if a user has the specified access permission for a file.
func (s *AclService) CanAccess(user string, file *FileInfo, action Action) (bool, error) {

	path := strings.TrimLeft(filepath.Clean(file.Path), PathSep)
	if IsOwner(path, user) {
		return true, nil
	}

	rule, err := s.GetNearestRule(path)
	if err != nil {
		return false, err
	} else if rule == nil {
		return false, fmt.Errorf("no rule found for path %s", path)
	}

	fileLimits := true
	isAcl := IsAclFile(path)
	// elevate action for ACL files
	if isAcl && action == ActionFileRead {
		action = ActionFileReadACL
	} else if isAcl && action == ActionFileWrite {
		action = ActionFileWriteACL
	} else if action == ActionFileWrite {
		// writes need to be checked against the file limits
		fileLimits = rule.WithinLimits(file)
	}

	return rule.CanAccess(user, action) && fileLimits, nil
}

// String returns a string representation of the ACL service's rule tree.
func (s *AclService) String() string {
	return s.tree.String()
}
