package sync

import (
	"time"
)

type SyncPath string // common relative path on the client and server

func (p SyncPath) String() string {
	return string(p)
}

type FileMetadata struct {
	Path         SyncPath
	Size         int64
	ETag         string
	Version      string
	LastModified time.Time
}
