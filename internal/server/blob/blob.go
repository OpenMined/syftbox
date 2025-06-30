package blob

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type BlobService struct {
	backend     *S3Backend
	index       *BlobIndex
	indexer     *blobIndexer
	callbacks   []BlobChangeCallback
	callbacksMu sync.RWMutex
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
func (b *BlobService) Backend() IBlobBackend {
	return b.backend
}

// Index returns the blob index
func (b *BlobService) Index() IBlobIndex {
	return b.index
}

// SetOnBlobChangeCallback sets the callback function for blob changes
func (b *BlobService) OnBlobChange(callback BlobChangeCallback) {
	b.callbacksMu.Lock()
	defer b.callbacksMu.Unlock()
	b.callbacks = append(b.callbacks, callback)
}

// invokeBlobChangeCallbacks invokes all registered callbacks with the given key and event type
func (b *BlobService) invokeBlobChangeCallbacks(key string, eventType BlobEventType) {
	b.callbacksMu.RLock()
	defer b.callbacksMu.RUnlock()

	for _, callback := range b.callbacks {
		go callback(key, eventType)
	}
}

// implements the AfterPutObjectHook
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
		// Call all blob change callbacks
		b.invokeBlobChangeCallbacks(resp.Key, BlobEventPut)
	}
}

// implements the AfterDeleteObjectHook
func (b *BlobService) afterDeleteObjects(req string, _ bool) {
	if err := b.index.Remove(req); err != nil {
		slog.Error("update index", "hook", "DeleteObject", "key", req, "error", err)
	} else {
		slog.Info("update index", "hook", "DeleteObject", "key", req)
		// Call all blob change callbacks
		b.invokeBlobChangeCallbacks(req, BlobEventDelete)
	}
}

// implements the AfterCopyObjectHook
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
		// Call all blob change callbacks
		b.invokeBlobChangeCallbacks(req.DestinationKey, BlobEventCopy)
	}

}

// soft check interface, incase we want to add a different implementation
var _ Service = (*BlobService)(nil)
