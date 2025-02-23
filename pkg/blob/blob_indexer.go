package blob

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

const IndexUpdatePeriod = 15 * time.Minute

type BlobStorageIndexer struct {
	api   *BlobStorageAPI
	index map[string]*BlobInfo
	mu    sync.RWMutex
}

func NewBlobIndexer(api *BlobStorageAPI) *BlobStorageIndexer {
	return &BlobStorageIndexer{
		api:   api,
		index: make(map[string]*BlobInfo),
	}
}

func (bi *BlobStorageIndexer) Start(ctx context.Context) error {
	// Initial build of the index
	if err := bi.buildIndex(ctx); err != nil {
		return err
	}

	// Start periodic updates
	go func() {
		slog.Debug("blob indexer started")
		ticker := time.NewTicker(IndexUpdatePeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Debug("blob indexer stopped")
				return
			case <-ticker.C:
				if err := bi.buildIndex(ctx); err != nil {
					slog.Error("blob index build", "error", err)
				}
			}
		}
	}()

	return nil
}

func (bi *BlobStorageIndexer) Get(key string) *BlobInfo {
	bi.mu.RLock()
	defer bi.mu.RUnlock()

	return bi.index[key]
}

func (bi *BlobStorageIndexer) Set(blob *BlobInfo) {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	bi.index[blob.Key] = blob
}

func (bi *BlobStorageIndexer) Remove(key string) {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	delete(bi.index, key)
}

func (bi *BlobStorageIndexer) List() []*BlobInfo {
	// return blobs
	return slices.SortedFunc(bi.Iter(), func(a, b *BlobInfo) int {
		return strings.Compare(a.Key, b.Key)
	})
}

func (bi *BlobStorageIndexer) Iter() iter.Seq[*BlobInfo] {
	bi.mu.RLock()
	defer bi.mu.RUnlock()

	return maps.Values(bi.index)
}

func (bi *BlobStorageIndexer) buildIndex(ctx context.Context) error {
	start := time.Now()

	blobs, err := bi.api.ListObjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}

	bi.mu.Lock()
	defer bi.mu.Unlock()

	// clear the index
	clear(bi.index)

	// build the index
	for _, blob := range blobs {
		bi.index[blob.Key] = blob
	}

	slog.Debug("blob indexer rebuild", "blbos", len(blobs), "took", time.Since(start))

	return nil
}
