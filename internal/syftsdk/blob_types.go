package syftsdk

import (
	"time"
)

// BlobInfo represents information about a blob
type BlobInfo struct {
	Key          string    `json:"key"`
	ETag         string    `json:"etag"`
	Size         int       `json:"size"`
	LastModified time.Time `json:"lastModified"`
}

// BlobURL represents a presigned URL for a blob
type BlobURL struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

// BlobError represents an error for a specific blob operation
type BlobError struct {
	APIError
	Key string `json:"key"`
}

// ===================================================================================================

// UploadParams represents the parameters for uploading a blob
type UploadParams struct {
	Key               string
	FilePath          string
	ChecksumCRC64NVME string
	Callback          func(uploadedBytes int64, totalBytes int64)
}

// UploadResponse represents the response from a blob upload
type UploadResponse struct {
	Key          string `json:"key"`
	Version      string `json:"version"`
	ETag         string `json:"etag"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
}

// ===================================================================================================

// PresignedParams represents the parameters for getting presigned URLs
type PresignedParams struct {
	Keys []string `json:"keys"`
}

// PresignedResponse represents the response from a presigned URL request
type PresignedResponse struct {
	URLs   []*BlobURL   `json:"urls"`
	Errors []*BlobError `json:"errors"`
}

// ===================================================================================================

// DeleteParams represents the parameters for deleting blobs
type DeleteParams struct {
	Keys []string `json:"keys"`
}

// DeleteResponse represents the response from a blob delete operation
type DeleteResponse struct {
	Deleted []string     `json:"deleted"`
	Errors  []*BlobError `json:"errors"`
}
