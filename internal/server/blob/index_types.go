package blob

import (
	"iter"
	"time"
)

// not enforced yet
type IBlobIndex interface {
	Get(key string) (*BlobInfo, bool)
	Set(blob *BlobInfo) error
	SetMany(blobs []*BlobInfo) error
	Remove(key string) error
	List() ([]*BlobInfo, error)
	Iter() iter.Seq[*BlobInfo]
	Count() int
	FilterByKeyGlob(pattern string) ([]*BlobInfo, error)
	FilterByPrefix(prefix string) ([]*BlobInfo, error)
	FilterBySuffix(suffix string) ([]*BlobInfo, error)
	FilterByTime(filter TimeFilter) ([]*BlobInfo, error)
	FilterAfterTime(after time.Time) ([]*BlobInfo, error)
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
