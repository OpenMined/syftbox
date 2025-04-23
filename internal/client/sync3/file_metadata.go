package sync3

import (
	"time"
)

type FileMetadata struct {
	Path         string
	Size         int64
	ETag         string
	Version      string
	LastModified time.Time
}
