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
	// LocalETag is the local content hash (plain MD5) captured at last successful sync.
	// It is persisted in the journal to compare local changes independent of server ETag format.
	LocalETag    string
	Version      string
	LastModified time.Time
}
