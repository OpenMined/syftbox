package blob

import (
	"context"
	"iter"

	"github.com/yashgorana/syftbox-go/pkg/acl"
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

func (b *BlobService) List() []*BlobInfo {
	return b.indexer.List()
}

func (b *BlobService) Iter() iter.Seq[*BlobInfo] {
	return b.indexer.Iter()
}

// Todo - might move this to datasite service
func (b *BlobService) ListAclFiles() []*BlobInfo {
	acls := make([]*BlobInfo, 0)
	for blob := range b.indexer.Iter() {
		if acl.IsAclFile(blob.Key) {
			acls = append(acls, blob)
		}
	}
	return acls
}

// GetClient returns the underlying BlobStorageClient instance
func (b *BlobService) GetClient() *BlobClient {
	return b.api
}
