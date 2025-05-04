package blob

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const indexInterval = 15 * time.Minute

// blobIndexer handles the periodic updating of the blob index
type blobIndexer struct {
	backend *S3Backend
	index   *BlobIndex
}

// newBlobIndexer creates a new indexer that updates the provided index
func newBlobIndexer(backend *S3Backend, index *BlobIndex) *blobIndexer {
	return &blobIndexer{
		backend: backend,
		index:   index,
	}
}

// Start begins the indexing process and starts periodic updates
func (bi *blobIndexer) Start(ctx context.Context) error {
	slog.Debug("blob indexer started")

	// Initial build of the index
	if err := bi.buildIndex(ctx); err != nil {
		return err
	}

	// Start periodic updates
	go func() {

		ticker := time.NewTicker(indexInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Debug("blob indexer stopped")
				return
			case <-ticker.C:
				if err := bi.buildIndex(ctx); err != nil {
					slog.Error("blob indexer error", "error", err)
				}
			}
		}
	}()

	return nil
}

// buildIndex incrementally updates the index by fetching objects and delegating to BlobIndex.BulkUpdate
func (bi *blobIndexer) buildIndex(ctx context.Context) error {
	start := time.Now()

	blobs, err := bi.backend.ListObjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}

	result, err := bi.index.bulkUpdate(blobs)
	if err != nil {
		return fmt.Errorf("failed to update index: %w", err)
	}

	// Log statistics
	slog.Debug("blob indexer update result",
		"total", len(blobs),
		"added", result.Added,
		"updated", result.Updated,
		"deleted", result.Deleted,
		"took", time.Since(start),
	)

	return nil
}
