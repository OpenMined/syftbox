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

type BlobIndex interface {
	Get(key string) (*BlobInfo, bool)
	Set(blob *BlobInfo)
	Remove(key string)
	List() []*BlobInfo
	Iter() iter.Seq[*BlobInfo]
	FilterByPrefix(prefix string) []*BlobInfo
}

type BlobIndexer struct {
	api   *BlobClient
	index map[string]*BlobInfo
	mu    sync.RWMutex
}

func NewBlobIndexer(api *BlobClient) *BlobIndexer {
	return &BlobIndexer{
		api:   api,
		index: make(map[string]*BlobInfo),
	}
}

func (bi *BlobIndexer) Start(ctx context.Context) error {
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

func (bi *BlobIndexer) Get(key string) (*BlobInfo, bool) {
	bi.mu.RLock()
	defer bi.mu.RUnlock()

	val, ok := bi.index[key]
	return val, ok
}

func (bi *BlobIndexer) Set(blob *BlobInfo) {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	bi.index[blob.Key] = blob
}

func (bi *BlobIndexer) Remove(key string) {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	delete(bi.index, key)
}

func (bi *BlobIndexer) List() []*BlobInfo {
	// return slices.SortedFunc(bi.Iter(), func(a, b *BlobInfo) int {
	// 	return strings.Compare(a.Key, b.Key)
	// })
	// return blobs
	return slices.Collect(bi.Iter())
}

func (bi *BlobIndexer) Iter() iter.Seq[*BlobInfo] {
	bi.mu.RLock()
	defer bi.mu.RUnlock()

	return maps.Values(bi.index)
}

func (bi *BlobIndexer) FilterByPrefix(prefix string) []*BlobInfo {
	bi.mu.RLock()
	defer bi.mu.RUnlock()

	blobs := make([]*BlobInfo, 0, len(bi.index))
	for _, blob := range bi.index {
		if strings.HasPrefix(blob.Key, prefix) {
			blobs = append(blobs, blob)
		}
	}
	return blobs
}

func (bi *BlobIndexer) buildIndex(ctx context.Context) error {
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

	slog.Debug("blob indexer rebuild", "blobs", len(blobs), "took", time.Since(start))

	return nil
}
