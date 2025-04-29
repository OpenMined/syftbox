package sync3

import (
	"time"
)

type FileMetadata struct {
	Path         string    `db:"path"`
	Size         int64     `db:"size"`
	ETag         string    `db:"etag"`
	Version      string    `db:"version"`
	LastModified time.Time `db:"last_modified"`
}
