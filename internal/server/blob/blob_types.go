package blob

// BlobEventType represents the type of blob operation that occurred
type BlobEventType uint8

// BlobChangeCallback is the function signature for blob change callbacks
type BlobChangeCallback func(key string, eventType BlobEventType)

// Service defines the minimal interface for blob operations.
// It provides access to the underlying backend storage, index management,
// and allows registering callbacks for blob change events.
type Service interface {
	// Backend returns the underlying blob storage backend
	Backend() IBlobBackend

	// Index returns the blob index for metadata management
	Index() IBlobIndex

	// OnBlobChange registers a callback for blob change events
	OnBlobChange(callback BlobChangeCallback)
}

// BlobEvent constants define the different types of blob operations that can trigger events.
const (
	BlobEventPut BlobEventType = 1 << iota
	BlobEventDelete
	BlobEventCopy
)
