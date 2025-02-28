package datasite

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/yashgorana/syftbox-go/pkg/acl"
	"github.com/yashgorana/syftbox-go/pkg/blob"
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

func (d *DatasiteService) Init(ctx context.Context) error {
	if err := d.blobSvc.Start(ctx); err != nil {
		return fmt.Errorf("error starting blob service: %w", err)
	}

	// Fetch the ACL files
	start := time.Now()
	acls := d.ListAclFiles()
	slog.Debug("acl list", "count", len(acls), "took", time.Since(start))

	// Fetch the ACL rulesets
	start = time.Now()
	ruleSets, err := d.fetchAcls(ctx, acls)
	if err != nil {
		return fmt.Errorf("error fetching acls: %w", err)
	}
	slog.Debug("acl read", "count", len(ruleSets), "took", time.Since(start))

	// Load the ACL rulesets
	start = time.Now()
	d.aclSvc.LoadRuleSets(ruleSets)
	slog.Debug("acl build", "count", len(ruleSets), "took", time.Since(start))

	// Warm up the ACL cache
	index := d.blobSvc.Index()
	start = time.Now()
	for blob := range index.Iter() {
		_, err := d.aclSvc.CanAccess(acl.Everyone, &acl.FileInfo{Path: blob.Key}, acl.ActionFileRead)
		if err != nil {
			slog.Error("acl cache warm error", "path", blob.Key, "error", err)
		}
	}
	slog.Debug("acl cache warm", "took", time.Since(start))

	return nil
}

func (d *DatasiteService) GetView(user string) []*blob.BlobInfo {
	// First collect all accessible blobs
	index := d.blobSvc.Index()
	blobs := index.List()
	view := make([]*blob.BlobInfo, 0, len(blobs))

	// Filter blobs based on ACL
	for _, blob := range blobs {
		ok, err := d.aclSvc.CanAccess(user, &acl.FileInfo{Path: blob.Key}, acl.ActionFileRead)
		if ok && err == nil {
			view = append(view, blob)
		}
	}

	return view
}

func (d *DatasiteService) DownloadFiles(ctx context.Context, user string, keys []string) ([]BlobUrl, []BlobError, error) {
	index := d.blobSvc.Index()
	client := d.blobSvc.Client()

	urls := make([]BlobUrl, 0, len(keys))
	errs := make([]BlobError, 0, len(keys))

	for _, key := range keys {

		_, ok := index.Get(key)
		if !ok {
			errs = append(errs, BlobError{Key: key, Error: "not found"})
			continue
		}

		ok, err := d.aclSvc.CanAccess(user, &acl.FileInfo{Path: key}, acl.ActionFileRead)
		if !ok || err != nil {
			errs = append(errs, BlobError{Key: key, Error: "access denied"})
			continue
		}

		url, err := client.PresignedDownload(ctx, key)
		if err != nil {
			errs = append(errs, BlobError{Key: key, Error: err.Error()})
			continue
		}

		urls = append(urls, BlobUrl{Key: key, Url: url})
	}

	return urls, errs, nil
}

func (d *DatasiteService) fetchAcls(ctx context.Context, aclBlobs []*blob.BlobInfo) ([]*acl.RuleSet, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := 8
	jobs := make(chan *blob.BlobInfo)
	results := make([]*acl.RuleSet, 0, len(aclBlobs))
	blobClient := d.blobSvc.Client()

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for blob := range jobs {

				// Pull the ACL file
				obj, err := blobClient.Download(ctx, blob.Key)
				if err != nil {
					slog.Error("ruleset fetch error", "path", blob.Key, "error", err)
					continue
				}

				// Parse the ACL file
				ruleset, err := acl.NewRuleSetFromReader(blob.Key, obj.Body)
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

func (d *DatasiteService) ListAclFiles() []*blob.BlobInfo {
	index := d.blobSvc.Index()

	acls := make([]*blob.BlobInfo, 0)
	for blob := range index.Iter() {
		if acl.IsAclFile(blob.Key) {
			acls = append(acls, blob)
		}
	}
	return acls
}
