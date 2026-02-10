package sync

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSyncJournal_ContentsChanged_ETagOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	j, err := NewSyncJournal(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Open(); err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	path := SyncPath("alice@example.com/public/a.txt")
	meta := &FileMetadata{
		Path:         path,
		ETag:         "etag1",
		Version:      "v1",
		Size:         10,
		LastModified: time.Now().Add(-time.Hour),
	}
	if err := j.Set(meta); err != nil {
		t.Fatal(err)
	}

	changed, err := j.ContentsChanged(path, "etag1")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected ContentsChanged=false when etag identical")
	}

	changed, err = j.ContentsChanged(path, "etag2")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected ContentsChanged=true when etag differs")
	}
}

