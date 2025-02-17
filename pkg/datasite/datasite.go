package datasite

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/yashgorana/syftbox-go/pkg/acl"
	"github.com/yashgorana/syftbox-go/pkg/blob"
)

type DatasiteService struct {
	blobSvc *blob.BlobStorageService
	aclSvc  *acl.AclService
}

func NewDatasiteService(blobSvc *blob.BlobStorageService, aclSvc *acl.AclService) *DatasiteService {
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
	acls := d.blobSvc.ListAclFiles()
	slog.Info("got acls", "acls", len(acls), "took", time.Since(start))

	// Fetch the ACL rulesets
	start = time.Now()
	ruleSets, err := d.fetchAcls(ctx, acls)
	if err != nil {
		return fmt.Errorf("error fetching acls: %w", err)
	}
	slog.Info("ruleset fetched", "acls", len(ruleSets), "took", time.Since(start))

	// Load the ACL rulesets
	start = time.Now()
	d.aclSvc.LoadRuleSets(ruleSets)
	slog.Info("ruleset added", "acls", len(ruleSets), "took", time.Since(start))

	// Warm up the ACL cache
	start = time.Now()
	for blob := range d.blobSvc.Iter() {
		_, err := d.aclSvc.CanAccess(acl.Everyone, &acl.FileInfo{Path: blob.Key}, acl.ActionFileRead)
		if err != nil {
			slog.Error("acl cache warm error", "path", blob.Key, "error", err)
		}
	}
	slog.Info("acl cache warmed", "took", time.Since(start))

	return nil
}

func (d *DatasiteService) GetView(user string) []*blob.BlobInfo {
	// First collect all accessible blobs
	view := make([]*blob.BlobInfo, 0)
	for blob := range d.blobSvc.Iter() {
		ok, err := d.aclSvc.CanAccess(user, &acl.FileInfo{Path: blob.Key}, acl.ActionFileRead)
		if ok && err == nil {
			view = append(view, blob)
		}
	}

	// Sort the view
	sort.Slice(view, func(i, j int) bool {
		return view[i].Key < view[j].Key
	})

	return view
}

func (d *DatasiteService) fetchAcls(ctx context.Context, aclBlobs []*blob.BlobInfo) ([]*acl.RuleSet, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := 8
	jobs := make(chan *blob.BlobInfo)
	results := make([]*acl.RuleSet, 0, len(aclBlobs))
	api := d.blobSvc.GetAPI()

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for blob := range jobs {

				// Pull the ACL file
				obj, err := api.Download(ctx, blob.Key)
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
