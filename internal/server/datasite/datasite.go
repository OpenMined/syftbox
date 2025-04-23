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
	blobSvc *blob.BlobService
	aclSvc  *acl.AclService
}

func NewDatasiteService(blobSvc *blob.BlobService, aclSvc *acl.AclService) *DatasiteService {
	return &DatasiteService{
		blobSvc: blobSvc,
		aclSvc:  aclSvc,
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
	d.aclSvc.LoadRuleSets(ruleSets)
	slog.Debug("acl build", "count", len(ruleSets), "took", time.Since(start))

	// Warm up the ACL cache
	start = time.Now()
	for blob := range d.blobSvc.Index().Iter() {
		if err := d.aclSvc.CanAccess(
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
	blobs, _ := d.blobSvc.Index().List()
	view := make([]*blob.BlobInfo, 0, len(blobs))

	// Filter blobs based on ACL
	for _, blob := range blobs {
		if err := d.aclSvc.CanAccess(
			&acl.User{ID: user, IsOwner: IsOwner(blob.Key, user)},
			&acl.File{Path: blob.Key},
			acl.AccessRead,
		); err == nil {
			view = append(view, blob)
		}
	}
	return view
}

// func (d *DatasiteService) DownloadFiles(ctx context.Context, user string, keys []string) ([]BlobUrl, []BlobError, error) {
// 	index := d.blobSvc.Index()
// 	client := d.blobSvc.Client()

// 	urls := make([]BlobUrl, 0, len(keys))
// 	errs := make([]BlobError, 0, len(keys))

// 	for _, key := range keys {

// 		_, ok := index.Get(key)
// 		if !ok {
// 			errs = append(errs, BlobError{Key: key, Error: "not found"})
// 			continue
// 		}

// 		if err := d.aclSvc.CanAccess(
// 			&acl.User{ID: user, IsOwner: IsOwner(key, user)},
// 			&acl.File{Path: key},
// 			acl.AccessRead,
// 		); err != nil {
// 			errs = append(errs, BlobError{Key: key, Error: err.Error()})
// 			continue
// 		}

// 		url, err := client.GetObjectPresigned(ctx, key)
// 		if err != nil {
// 			errs = append(errs, BlobError{Key: key, Error: err.Error()})
// 			continue
// 		}

// 		urls = append(urls, BlobUrl{Key: key, Url: url})
// 	}

// 	return urls, errs, nil
// }

func (d *DatasiteService) ListAclFiles() ([]*blob.BlobInfo, error) {
	return d.blobSvc.Index().FilterBySuffix(aclspec.AclFileName)
}

func (d *DatasiteService) fetchAcls(ctx context.Context, aclBlobs []*blob.BlobInfo) ([]*aclspec.RuleSet, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := 16
	jobs := make(chan *blob.BlobInfo)
	results := make([]*aclspec.RuleSet, 0, len(aclBlobs))
	blobClient := d.blobSvc.Client()

	slog.Debug("acl fetch", "workers", workers, "blobs", len(aclBlobs))

	// Start workers
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for blob := range jobs {

				// Pull the ACL file
				obj, err := blobClient.GetObject(ctx, blob.Key)
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
