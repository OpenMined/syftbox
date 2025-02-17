package blob

import (
	"context"
	"iter"

	"github.com/yashgorana/syftbox-go/pkg/acl"
)

type BlobStorageService struct {
	config  *BlobStorageConfig
	api     *BlobStorageAPI
	indexer *BlobStorageIndexer

	initialized bool
}

func NewBlobStorageService(cfg *BlobStorageConfig) *BlobStorageService {
	api := NewBlobStorageAPIFromConfig(cfg)
	return &BlobStorageService{
		config:  cfg,
		api:     api,
		indexer: NewBlobIndexer(api),
	}
}

func (b *BlobStorageService) Start(ctx context.Context) error {
	if b.initialized {
		return nil
	}
	b.initialized = true
	return b.indexer.Start(ctx)
}

func (b *BlobStorageService) ListFiles() []*BlobInfo {
	return b.indexer.List()
}

func (b *BlobStorageService) Iter() iter.Seq[*BlobInfo] {
	return b.indexer.Iter()
}

// Todo - might move this to datasite service
func (b *BlobStorageService) ListAclFiles() []*BlobInfo {
	acls := make([]*BlobInfo, 0)
	for blob := range b.indexer.Iter() {
		if acl.IsAclFile(blob.Key) {
			acls = append(acls, blob)
		}
	}
	return acls
}

// GetAPI returns the underlying BlobAPI instance
func (b *BlobStorageService) GetAPI() *BlobStorageAPI {
	return b.api
}
