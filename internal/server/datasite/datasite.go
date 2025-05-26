package datasite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
)

type DatasiteService struct {
	blob *blob.BlobService
	acl  *acl.ACLService
}

func NewDatasiteService(blobSvc *blob.BlobService, aclSvc *acl.ACLService) *DatasiteService {
	return &DatasiteService{
		blob: blobSvc,
		acl:  aclSvc,
	}
}

func (d *DatasiteService) Start(ctx context.Context) error {
	slog.Debug("datasite service start")
	// Fetch the ACL files
	start := time.Now()
	acls, err := d.ListAclFiles()
	if err != nil {
		return fmt.Errorf("error listing acls: %w", err)
	}
	slog.Debug("acl list", "count", len(acls), "took", time.Since(start))

	// Fetch the ACL rulesets
	start = time.Now()
	ruleSets, err := d.fetchAcls(ctx, acls)
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
	d.acl.AddRuleSets(ruleSets)
	slog.Debug("acl build", "count", len(ruleSets), "took", time.Since(start))

	// Warm up the ACL cache
	start = time.Now()
	for blob := range d.blob.Index().Iter() {
		if err := d.acl.CanAccess(
			&acl.User{ID: aclspec.Everyone},
			&acl.File{Path: blob.Key},
			acl.AccessRead,
		); err != nil && errors.Is(err, acl.ErrNoRuleFound) {
			slog.Warn("acl cache warm error", "path", blob.Key, "error", err)
		}
	}
	slog.Debug("acl cache warm", "took", time.Since(start))

	return nil
}

func (d *DatasiteService) Shutdown(ctx context.Context) error {
	slog.Debug("datasite service shutdown")
	return nil
}

func (d *DatasiteService) GetView(user string) []*blob.BlobInfo {
	// First collect all accessible blobs
	blobs, _ := d.blob.Index().List()
	view := make([]*blob.BlobInfo, 0, len(blobs))

	// Filter blobs based on ACL
	for _, blob := range blobs {
		if IsOwner(blob.Key, user) {
			view = append(view, blob)
			continue
		}

		if err := d.acl.CanAccess(
			&acl.User{ID: user},
			&acl.File{Path: blob.Key},
			acl.AccessRead,
		); err == nil {
			view = append(view, blob)
		}
	}
	return view
}

func (d *DatasiteService) ListAclFiles() ([]*blob.BlobInfo, error) {
	return d.blob.Index().FilterBySuffix(aclspec.AclFileName)
}

func (d *DatasiteService) fetchAcls(ctx context.Context, aclBlobs []*blob.BlobInfo) ([]*aclspec.RuleSet, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := 16
	jobs := make(chan *blob.BlobInfo)
	results := make([]*aclspec.RuleSet, 0, len(aclBlobs))
	blobBackend := d.blob.Backend()

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
