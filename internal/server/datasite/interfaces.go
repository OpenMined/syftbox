package datasite

import (
	"github.com/openmined/syftbox/internal/server/blob"
)

// BlobServiceInterface defines the minimal interface we need from BlobService
type BlobServiceInterface interface {
	Backend() blob.BlobBackend
	Index() *blob.BlobIndex
	SetOnBlobChangeCallback(callback func(key string))
}

// Ensure BlobService implements our interface
var _ BlobServiceInterface = (*blob.BlobService)(nil)