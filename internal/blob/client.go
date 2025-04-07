package blob

import (
	"context"
	"io"
	"time"
)

type IBlobClient interface {
	GetObject(ctx context.Context, key string) (*GetObjectResponse, error)
	GetObjectPresigned(ctx context.Context, key string) (string, error)
	PutObject(ctx context.Context, params *PutObjectParams) (*PutObjectResponse, error)
	PutObjectPresigned(ctx context.Context, key string) (string, error)
	PutObjectMultipart(ctx context.Context, params *PutObjectMultipartParams) (*PutObjectMultipartResponse, error)
	CompleteMultipartUpload(ctx context.Context, params *CompleteMultipartUploadParams) (*PutObjectResponse, error)
	CopyObject(ctx context.Context, params *CopyObjectParams) (*CopyObjectResponse, error)
	DeleteObject(ctx context.Context, key string) (bool, error)
	ListObjects(ctx context.Context) ([]*BlobInfo, error)
}

// ===================================================================================================

type GetObjectResponse struct {
	Body         io.ReadCloser
	ETag         string
	Size         int64
	LastModified time.Time
}

// ===================================================================================================

type PutObjectParams struct {
	Key  string
	ETag string
	Size int64
	Body io.Reader
}

type PutObjectResponse struct {
	Key          string
	Version      string
	ETag         string
	Size         int64
	LastModified time.Time
}

type PutObjectPresignedResponse struct {
	Name          string   `json:"name" binding:"required"`
	UploadID      string   `json:"uploadId"`
	PresignedURLs []string `json:"presignedUrls"`
}

// ===================================================================================================

type PutObjectMultipartParams struct {
	Key   string `json: "key" form: "key" binding:"required"`
	Parts uint16 `json: "parts" form: "parts" binding:"required"`
}

type PutObjectMultipartResponse struct {
	Key      string   `json:"key"`
	UploadID string   `json:"uploadId"`
	URLs     []string `json:"urls"`
}

// ===================================================================================================

type CompletedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

type CompleteMultipartUploadParams struct {
	Key      string           `json:"key"`
	UploadID string           `json:"uploadId"`
	Parts    []*CompletedPart `json:"parts"`
}

// ===================================================================================================

type CopyObjectParams struct {
	SourceKey      string
	DestinationKey string
}

type CopyObjectResponse struct {
	ETag         string
	LastModified time.Time
}

// ===================================================================================================

type BlobInfo struct {
	Key          string `json:"key" db:"key"`
	ETag         string `json:"etag" db:"etag"`
	Size         int64  `json:"size" db:"size"`
	LastModified string `json:"lastModified" db:"last_modified"`
}
