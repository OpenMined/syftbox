package blob

import (
	"fmt"

	"github.com/openmined/syftbox/internal/server/handlers/api"
)

type BlobURL struct {
	Key string `json:"key"`
	Url string `json:"url"`
}

type BlobAPIError struct {
	api.SyftAPIError
	Key string `json:"key"`
}

func NewBlobAPIError(code string, message string, key string) *BlobAPIError {
	return &BlobAPIError{
		Key: key,
		SyftAPIError: api.SyftAPIError{
			Code:    code,
			Message: message,
		},
	}
}

func (e *BlobAPIError) Error() string {
	return fmt.Sprintf("syft api blob error: code=%s, message=%s, key=%s", e.Code, e.Message, e.Key)
}

type UploadRequest struct {
	Key string `form:"key" binding:"required"`
	// MD5       string `form:"md5"`
	// CRC64NVME string `form:"crc64nvme"`
	// CRC32C    string `form:"crc32c"`
	// SHA256    string `form:"sha256"`
}

type UploadResponse struct {
	Key          string `json:"key"`
	Version      string `json:"version"`
	ETag         string `json:"etag"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
}

type PresignURLRequest struct {
	Keys []string `json:"keys" binding:"required,min=1"`
}

type PresignURLResponse struct {
	URLs   []*BlobURL      `json:"urls"`
	Errors []*BlobAPIError `json:"errors"`
}

type DeleteRequest struct {
	Keys []string `json:"keys" binding:"required,min=1"`
}

type DeleteResponse struct {
	Deleted []string        `json:"deleted"`
	Errors  []*BlobAPIError `json:"errors"`
}

// Multipart upload types
type MultipartUploadRequest struct {
	Key   string `json:"key" binding:"required"`
	Parts int    `json:"parts" binding:"required,min=1,max=10000"`
}

type MultipartUploadResponse struct {
	Key      string   `json:"key"`
	UploadID string   `json:"uploadId"`
	URLs     []string `json:"urls"`
}

type CompleteUploadRequest struct {
	Key      string                 `json:"key" binding:"required"`
	UploadID string                 `json:"uploadId" binding:"required"`
	Parts    []CompletedPartRequest `json:"parts" binding:"required,min=1"`
}

type CompletedPartRequest struct {
	PartNumber int    `json:"partNumber" binding:"required,min=1"`
	ETag       string `json:"etag" binding:"required"`
}

type CompleteUploadResponse struct {
	Key          string `json:"key"`
	Version      string `json:"version"`
	ETag         string `json:"etag"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
}
