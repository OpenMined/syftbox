package blob

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

type BlobService struct {
	backend *S3Backend
	index   *BlobIndex
	indexer *blobIndexer
}

func NewBlobService(cfg *S3Config, db *sqlx.DB) (*BlobService, error) {
	index, err := newBlobIndex(db)
	if err != nil {
		return nil, err
	}

	svc := &BlobService{}
	svc.index = index
	svc.backend = NewS3BackendWithConfig(cfg)
	svc.indexer = newBlobIndexer(svc.backend, svc.index)

	return svc, nil
}

// NewS3BucketConfig creates a configuration for an S3 bucket
// func WithS3Config(bucketName, region, accessKey, secretKey string, accelerate bool) *S3Config {
// 	return &S3Config{
// 		BucketName:    bucketName,
// 		Region:        region,
// 		AccessKey:     accessKey,
// 		SecretKey:     secretKey,
// 		UseAccelerate: accelerate,
// 	}
// }

// NewMinioBucketConfig creates a configuration for a Minio bucket
// func WithMinioConfig(url, bucketName, accessKey, secretKey string) *S3Config {
// 	return &S3Config{
// 		BucketName:    bucketName,
// 		Endpoint:      url,
// 		Region:        "us-east-1",
// 		AccessKey:     accessKey,
// 		SecretKey:     secretKey,
// 		UseAccelerate: false,
// 	}
// }

func (b *BlobService) Start(ctx context.Context) error {
	slog.Debug("blob service start")
	b.backend.setHooks(&blobBackendHooks{
		AfterPutObject:    b.afterPutObject,
		AfterDeleteObject: b.afterDeleteObjects,
		AfterCopyObject:   b.afterCopyObject,
	})
	return b.indexer.Start(ctx)
}

// Shutdown releases any resources used by the service
func (b *BlobService) Shutdown(ctx context.Context) error {
	slog.Debug("blob service shutdown")
	return b.index.Close()
}

// Backend returns the underlying blob backend instance
func (b *BlobService) Backend() BlobBackend {
	return b.backend
}

// Index returns the blob index
func (b *BlobService) Index() *BlobIndex {
	return b.index
}

// getHooks returns the backend hooks (assumes S3Backend for now)
func (b *BlobService) getHooks() *blobBackendHooks {
	return b.backend.hooks
}

// SetOnBlobChangeCallback sets the callback function for blob changes
func (b *BlobService) SetOnBlobChangeCallback(callback func(key string)) {
	if hooks := b.getHooks(); hooks != nil {
		hooks.OnBlobChange = callback
	}
}

func (b *BlobService) afterPutObject(_ *PutObjectParams, resp *PutObjectResponse) {
	if err := b.index.Set(&BlobInfo{
		Key:          resp.Key,
		ETag:         resp.ETag,
		Size:         resp.Size,
		LastModified: resp.LastModified.Format(time.RFC3339),
	}); err != nil {
		slog.Error("update index", "hook", "PutObject", "key", resp.Key, "error", err)
	} else {
		slog.Info("update index", "hook", "PutObject", "key", resp.Key)
	}
	
	// Call blob change callback if set
	if hooks := b.getHooks(); hooks != nil && hooks.OnBlobChange != nil {
		hooks.OnBlobChange(resp.Key)
	}
}

func (b *BlobService) afterDeleteObjects(req string, _ bool) {
	if err := b.index.Remove(req); err != nil {
		slog.Error("update index", "hook", "DeleteObject", "key", req, "error", err)
	} else {
		slog.Info("update index", "hook", "DeleteObject", "key", req)
	}
}

func (b *BlobService) afterCopyObject(req *CopyObjectParams, resp *CopyObjectResponse) {
	if err := b.index.Set(&BlobInfo{
		Key:          req.DestinationKey,
		ETag:         resp.ETag,
		Size:         0,
		LastModified: resp.LastModified.Format(time.RFC3339),
	}); err != nil {
		slog.Error("update index", "hook", "CopyObject", "src", req.SourceKey, "dest", req.DestinationKey, "error", err)
	} else {
		slog.Info("update index", "hook", "CopyObject", "src", req.SourceKey, "dest", req.DestinationKey)
	}
}
