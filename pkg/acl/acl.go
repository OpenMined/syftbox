package acl

import (
	"time"
)

type FileInfo struct {
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      uint64
	ModTime   time.Time
	Crc32     uint32
	Hash      string
}

type Action uint8

const (
	ActionFileRead Action = 1 << iota
	ActionFileWrite
	ActionFileWriteACL
)

type AclService struct {
	tree  *aclTree
	cache *aclRuleCache
}

func NewAclService() *AclService {
	return &AclService{
		tree:  newAclTree(),
		cache: newAclRuleCache(),
	}
}

func (s *AclService) AddRuleSet(path string, ruleset *RuleSet) error {
	return s.tree.AddRuleSet(path, ruleset)
}

func (s *AclService) CanAccess(user string, file *FileInfo, action Action) bool {
	cached := s.cache.Get(file.Path)   // O(1)
	node := s.tree.FindNode(file.Path) // O(depth)

	var rule *aclRule

	if cached != nil && node.Equal(cached.node) {
		// cache hit
		rule = cached
	} else {
		// cache miss
		rule = node.FindBestRule(file.Path) // O(rules|node) a.k.a static
		s.cache.Set(file.Path, rule)        // O(1)
	}

	fileLimits := true

	if action == ActionFileWrite {
		// permissions writes need Admin previliges
		if isAclFile(file.Path) {
			action = ActionFileWriteACL
		}

		// writes need to be checked against the file limits
		fileLimits = rule.WithinLimts(file)
	}

	// todo - check rule.WithinLimts(info) if write
	return rule.CanAccess(user, action) && fileLimits
}

func (s *AclService) DebugTree() string {
	return s.tree.String()
}
