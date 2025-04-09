package blob

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

type BlobService struct {
	config  *S3BlobConfig
	client  *BlobClient
	index   *BlobIndex
	indexer *blobIndexer
}

func NewBlobService(cfg *S3BlobConfig, idxCfg *IndexConfig) (*BlobService, error) {
	index, err := createIndex(idxCfg)
	if err != nil {
		return nil, err
	}

	svc := &BlobService{}
	svc.config = cfg
	svc.index = index
	svc.client = NewBlobClientWithS3Config(svc.config)
	svc.indexer = newBlobIndexer(svc.client, svc.index)

	return svc, nil
}

func createIndex(idxCfg *IndexConfig) (*BlobIndex, error) {
	if idxCfg == nil {
		return newBlobIndex()
	} else if idxCfg.DBPath != "" {
		return newBlobIndexFromPath(idxCfg.DBPath)
	} else if idxCfg.DB != nil {
		return newBlobIndexFromDB(idxCfg.DB)
	}
	return nil, fmt.Errorf("invalid index configuration")
}

// NewS3BucketConfig creates a configuration for an S3 bucket
func WithS3Config(bucketName, region, accessKey, secretKey string, accelerate bool) *S3BlobConfig {
	return &S3BlobConfig{
		BucketName:    bucketName,
		Region:        region,
		AccessKey:     accessKey,
		SecretKey:     secretKey,
		UseAccelerate: accelerate,
	}
}

// NewMinioBucketConfig creates a configuration for a Minio bucket
func WithMinioConfig(url, bucketName, accessKey, secretKey string) *S3BlobConfig {
	return &S3BlobConfig{
		BucketName:    bucketName,
		Endpoint:      url,
		Region:        "us-east-1",
		AccessKey:     accessKey,
		SecretKey:     secretKey,
		UseAccelerate: false,
	}
}

func WithDBPath(path string) *IndexConfig {
	return &IndexConfig{
		DBPath: path,
	}
}

// WithDB creates an index configuration using an existing DB connection
func WithDB(db *sqlx.DB) *IndexConfig {
	return &IndexConfig{
		DB: db,
	}
}

func (b *BlobService) Start(ctx context.Context) error {
	b.client.setHooks(&blobClientHooks{
		AfterPutObject:    b.afterPutObject,
		AfterDeleteObject: b.afterDeleteObjects,
		AfterCopyObject:   b.afterCopyObject,
	})
	return b.indexer.Start(ctx)
}

// Close releases any resources used by the service
func (b *BlobService) Close() error {
	return b.index.Close()
}

// Client returns the underlying BlobClient instance
func (b *BlobService) Client() *BlobClient {
	return b.client
}

// Index returns the blob index
func (b *BlobService) Index() *BlobIndex {
	return b.index
}

func (b *BlobService) afterPutObject(_ *PutObjectParams, resp *PutObjectResponse) {
	slog.Info("update index put object", "key", resp.Key)
	b.index.Set(&BlobInfo{
		Key:          resp.Key,
		ETag:         resp.ETag,
		Size:         resp.Size,
		LastModified: resp.LastModified.Format(time.RFC3339),
	})
}

func (b *BlobService) afterDeleteObjects(req string, _ bool) {
	slog.Info("update index delete object", "key", req)
	b.index.Remove(req)
}

func (b *BlobService) afterCopyObject(req *CopyObjectParams, resp *CopyObjectResponse) {
	slog.Info("update index copy object", "dest", req.DestinationKey)
	b.index.Set(&BlobInfo{
		Key:          req.DestinationKey,
		ETag:         resp.ETag,
		Size:         0,
		LastModified: resp.LastModified.Format(time.RFC3339),
	})
}
