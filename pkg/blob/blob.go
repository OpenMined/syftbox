package blob

import (
	"context"
)

type BlobService struct {
	config  *BlobConfig
	api     *BlobClient
	indexer *BlobIndexer
}

func NewBlobService(cfg *BlobConfig) *BlobService {
	api := NewBlobClientWithConfig(cfg)
	return &BlobService{
		config:  cfg,
		api:     api,
		indexer: NewBlobIndexer(api),
	}
}

func (b *BlobService) Start(ctx context.Context) error {
	return b.indexer.Start(ctx)
}

// Client returns the underlying BlobClient instance
func (b *BlobService) Client() *BlobClient {
	return b.api
}

func (b *BlobService) Index() BlobIndex {
	return b.indexer
}
