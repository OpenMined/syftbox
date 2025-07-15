package acl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/blob"
)

// ACLService helps to manage and enforce access control rules for file system operations.
type ACLService struct {
	blob  blob.Service
	tree  *ACLTree
	cache *ACLCache
}

// NewACLService creates a new ACL service instance
func NewACLService(blob blob.Service) *ACLService {
	return &ACLService{
		blob:  blob,
		tree:  NewACLTree(),
		cache: NewACLCache(),
	}
}

func (s *ACLService) Start(ctx context.Context) error {
	slog.Debug("acl service start")

	// Fetch the ACL files
	start := time.Now()
	acls, err := s.blob.Index().FilterBySuffix(aclspec.ACLFileName)
	if err != nil {
		return fmt.Errorf("error listing acls: %w", err)
	}
	slog.Debug("acl list", "count", len(acls), "took", time.Since(start))

	// Fetch the ACL rulesets
	start = time.Now()
	ruleSets, err := s.fetchAcls(ctx, acls)
	if err != nil {
		return fmt.Errorf("error fetching acls: %w", err)
	}
	slog.Debug("acl fetch", "count", len(ruleSets), "took", time.Since(start))

	if len(ruleSets) == 0 {
		slog.Warn("no ACL rulesets found")
		return nil
	}

	// Load the ACL rulesets
	start = time.Now()
	for _, ruleSet := range ruleSets {
		if _, err := s.AddRuleSet(ruleSet); err != nil {
			slog.Warn("ruleset update error", "path", ruleSet.Path, "error", err)
		}
	}
	slog.Debug("acl build", "count", len(ruleSets), "took", time.Since(start))

	// Warm up the ACL cache
	start = time.Now()
	for blob := range s.blob.Index().Iter() {
		if err := s.CanAccess(
			&User{ID: aclspec.Everyone},
			&File{Path: blob.Key},
			AccessRead,
		); err != nil && errors.Is(err, ErrNoRule) {
			slog.Warn("acl cache warm error", "path", blob.Key, "error", err)
		}
	}
	slog.Debug("acl cache warm", "took", time.Since(start))

	s.blob.OnBlobChange(s.onBlobChange)

	return nil
}

func (s *ACLService) Shutdown(ctx context.Context) error {
	slog.Debug("acl service shutdown")
	return nil
}

// AddRuleSet adds or updates a new set of rules to the service.
func (s *ACLService) AddRuleSet(ruleSet *aclspec.RuleSet) (ACLVersion, error) {
	node, err := s.tree.AddRuleSet(ruleSet)
	if err != nil {
		return 0, err
	}

	deleted := s.cache.DeletePrefix(ruleSet.Path)
	slog.Debug("updated rule set", "path", node.path, "version", node.version, "cache.deleted", deleted, "cache.size", s.cache.Count(), "blob.size", s.blob.Index().Count())
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

// fetchAcls fetches the ACL rulesets from the blob storage
func (s *ACLService) fetchAcls(ctx context.Context, aclBlobs []*blob.BlobInfo) ([]*aclspec.RuleSet, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := 16
	jobs := make(chan *blob.BlobInfo)
	results := make([]*aclspec.RuleSet, 0, len(aclBlobs))
	blobBackend := s.blob.Backend()

	slog.Debug("acl fetch", "workers", workers, "blobs", len(aclBlobs))

	// Start workers
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for blob := range jobs {

				// Pull the ACL file
				obj, err := blobBackend.GetObject(ctx, blob.Key)
				if err != nil {
					slog.Error("ruleset fetch error", "path", blob.Key, "error", err)
					continue
				}

				// Parse the ACL file
				ruleset, err := aclspec.LoadFromReader(blob.Key, obj.Body)
				obj.Body.Close()
				if err != nil {
					slog.Error("ruleset parse error", "path", blob.Key, "error", err)
					continue
				}

				// Append the ruleset to the results
				mu.Lock()
				results = append(results, ruleset)
				mu.Unlock()
			}
		}()
	}

	// Send work to workers
	for _, blob := range aclBlobs {
		jobs <- blob
	}
	close(jobs)
	wg.Wait()

	return results, nil
}

func (s *ACLService) onBlobChange(key string, eventType blob.BlobEventType) {
	if eventType == blob.BlobEventDelete {
		// Clean up cache entry for the deleted file
		s.cache.Delete(key)
		slog.Debug("acl cache removed", "key", key, "cache.size", s.cache.Count(), "blob.size", s.blob.Index().Count())
	}
}

// checks if the user is the owner of the path
func isOwner(path string, user string) bool {
	path = ACLNormPath(path)
	return strings.HasPrefix(path, user)
}

var _ Service = (*ACLService)(nil)
