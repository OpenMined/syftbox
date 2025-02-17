// Package acl provides access control list functionality for file system operations.
package acl

import (
	"fmt"
	"time"
)

// FileInfo represents metadata about a file including its path, type, size, and hash information.
type FileInfo struct {
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      uint64
	ModTime   time.Time
	Crc32     uint32
	Hash      string
}

// Action represents a permission bit flag for different file operations.
type Action uint8

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

func (s *AclService) LoadRuleSets(ruleSets []*RuleSet) {
	for _, ruleSet := range ruleSets {
		s.AddRuleSet(ruleSet)
	}
}

// AddRuleSet adds a new set of rules to the service.
func (s *AclService) AddRuleSet(ruleset *RuleSet) error {
	return s.tree.AddRuleSet(ruleset)
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
	var rule *aclRule

	cached := s.cache.Get(path)                        // O(1)
	node, err := s.tree.FindNearestNodeWithRules(path) // O(depth)
	if err != nil {
		return nil, fmt.Errorf("failed to find node with rules: %w", err)
	}

	if cached != nil && node.Equal(cached.node) {
		// cache hit
		rule = cached
	} else {
		// cache miss
		//! this can be nil - and it's likely because the schema load is buggy
		rule = node.FindBestRule(path) // O(rules|node)
		s.cache.Set(path, rule)        // O(1)
	}

	return rule, nil
}

// CanAccess checks if a user has the specified access permission for a file.
func (s *AclService) CanAccess(user string, file *FileInfo, action Action) (bool, error) {
	fileLimits := true
	isAcl := IsAclFile(file.Path)

	rule, err := s.GetNearestRule(file.Path)
	if err != nil {
		return false, err
	} else if rule == nil {
		//! this was because node.FindBestRule returned nil.
		return false, nil
	}

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
