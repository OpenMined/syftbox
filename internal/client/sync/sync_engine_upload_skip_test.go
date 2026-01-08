package sync

import "testing"

func TestShouldSkipUpload_RemoteETagMatchesLocal(t *testing.T) {
	op := &SyncOperation{
		RelPath: "alice@example.com/public/a.txt",
		Local:   &FileMetadata{ETag: "same"},
		Remote:  &FileMetadata{ETag: "same"},
	}
	if skip, reason := shouldSkipUpload(op, "alice@example.com"); !skip || reason == "" {
		t.Fatalf("expected skip for matching etags, got skip=%v reason=%q", skip, reason)
	}
}

func TestShouldSkipUpload_DifferentETagDoesNotSkip(t *testing.T) {
	op := &SyncOperation{
		RelPath: "alice@example.com/public/a.txt",
		Local:   &FileMetadata{ETag: "local"},
		Remote:  &FileMetadata{ETag: "remote"},
	}
	if skip, _ := shouldSkipUpload(op, "alice@example.com"); skip {
		t.Fatal("expected not to skip for differing etags")
	}
}

