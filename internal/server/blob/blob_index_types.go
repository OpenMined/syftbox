package blob

import (
	"iter"
	"time"
)

// IBlobIndex defines the interface for blob index operations.
// It provides methods for managing and querying blob metadata in an index,
// including CRUD operations, filtering, and iteration capabilities.
// The interface is designed to be implemented by different index backends
// such as in-memory maps, databases, or other storage systems.
type IBlobIndex interface {
	// Get retrieves a blob by its key, returning the blob info and whether it exists
	Get(key string) (*BlobInfo, bool)

	// Set stores a blob in the index, creating or updating the entry
	Set(blob *BlobInfo) error

	// SetMany stores multiple blobs in the index in a single operation
	SetMany(blobs []*BlobInfo) error

	// Remove deletes a blob from the index by its key
	Remove(key string) error

	// List returns all blobs in the index as a slice
	List() ([]*BlobInfo, error)

	// Iter returns an iterator for traversing all blobs in the index
	Iter() iter.Seq[*BlobInfo]

	// Count returns the total number of blobs in the index
	Count() int

	// FilterByKeyGlob filters blobs by key using glob pattern matching
	FilterByKeyGlob(pattern string) ([]*BlobInfo, error)

	// FilterByPrefix filters blobs by key prefix
	FilterByPrefix(prefix string) ([]*BlobInfo, error)

	// FilterBySuffix filters blobs by key suffix
	FilterBySuffix(suffix string) ([]*BlobInfo, error)

	// FilterByTime filters blobs based on a time range filter
	FilterByTime(filter TimeFilter) ([]*BlobInfo, error)

	// FilterAfterTime filters blobs modified after the specified time
	FilterAfterTime(after time.Time) ([]*BlobInfo, error)

	// FilterBeforeTime filters blobs modified before the specified time
	FilterBeforeTime(before time.Time) ([]*BlobInfo, error)
}

type TimeFilter struct {
	Before *time.Time
	After  *time.Time
}

// bulkUpdateResult contains statistics about a bulk update operation
type bulkUpdateResult struct {
	Added   int
	Updated int
	Deleted int
}
