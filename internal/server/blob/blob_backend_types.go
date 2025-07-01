package blob

import (
	"context"
	"io"
	"time"
)

// Hook function signatures
type AfterPutObjectHook func(req *PutObjectParams, resp *PutObjectResponse)
type AfterCopyObjectHook func(req *CopyObjectParams, resp *CopyObjectResponse)
type AfterDeleteObjectHook func(req string, resp bool)

// blobBackendHooks provides notification hooks from blob backend operations (like S3) to the blob service
type blobBackendHooks struct {
	AfterPutObject    AfterPutObjectHook
	AfterCopyObject   AfterCopyObjectHook
	AfterDeleteObject AfterDeleteObjectHook
}

// IBlobBackend defines the interface for blob storage backend operations.
// It provides methods for basic CRUD operations on blob objects including
// single-part uploads, multipart uploads, presigned URLs, and object management.
// The interface is designed to be implemented by different storage backends
// such as S3, local filesystem, or other cloud storage providers.
type IBlobBackend interface {
	// GetObject retrieves an object from storage by its key
	GetObject(ctx context.Context, key string) (*GetObjectResponse, error)

	// GetObjectPresigned generates a presigned URL for downloading an object
	GetObjectPresigned(ctx context.Context, key string) (string, error)

	// PutObject uploads a single object to storage
	PutObject(ctx context.Context, params *PutObjectParams) (*PutObjectResponse, error)

	// PutObjectPresigned generates a presigned URL for uploading an object
	PutObjectPresigned(ctx context.Context, key string) (string, error)

	// PutObjectMultipart initiates a multipart upload and returns upload URLs
	PutObjectMultipart(ctx context.Context, params *PutObjectMultipartParams) (*PutObjectMultipartResponse, error)

	// CompleteMultipartUpload finalizes a multipart upload
	CompleteMultipartUpload(ctx context.Context, params *CompleteMultipartUploadParams) (*PutObjectResponse, error)

	// CopyObject copies an object from one location to another
	CopyObject(ctx context.Context, params *CopyObjectParams) (*CopyObjectResponse, error)

	// DeleteObject removes an object from storage, returns true if successful
	DeleteObject(ctx context.Context, key string) (bool, error)

	// ListObjects returns a list of all objects in storage
	ListObjects(ctx context.Context) ([]*BlobInfo, error)

	// Delegate returns the underlying backend implementation
	Delegate() any

	// setHooks sets the notification hooks for backend operations
	// This is a private method used internally by the blob service
	setHooks(hooks *blobBackendHooks)
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
	Key   string `json:"key" binding:"required"`
	Parts uint16 `json:"parts" binding:"required"`
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
