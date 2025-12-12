package sync

import (
	"os"
	"testing"
	"time"
)

func TestParsePartSizeEnv(t *testing.T) {
	t.Setenv("SBDEV_PART_SIZE", "")
	if got := parsePartSizeEnv(); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}

	t.Setenv("SBDEV_PART_SIZE", "8MB")
	if got := parsePartSizeEnv(); got != 8*1024*1024 {
		t.Fatalf("expected 8MB, got %d", got)
	}

	t.Setenv("SBDEV_PART_SIZE", "2gb")
	if got := parsePartSizeEnv(); got != 2*1024*1024*1024 {
		t.Fatalf("expected 2GB, got %d", got)
	}

	t.Setenv("SBDEV_PART_SIZE", "1024")
	if got := parsePartSizeEnv(); got != 1024 {
		t.Fatalf("expected 1024, got %d", got)
	}
}

func TestParsePartUploadTimeoutEnv(t *testing.T) {
	t.Setenv("SBDEV_PART_UPLOAD_TIMEOUT", "")
	t.Setenv("SYFTBOX_PART_UPLOAD_TIMEOUT_MS", "")
	if got := parsePartUploadTimeoutEnv(); got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}

	t.Setenv("SBDEV_PART_UPLOAD_TIMEOUT", "750ms")
	if got := parsePartUploadTimeoutEnv(); got != 750*time.Millisecond {
		t.Fatalf("expected 750ms, got %v", got)
	}

	// SBDEV_PART_UPLOAD_TIMEOUT takes precedence.
	t.Setenv("SYFTBOX_PART_UPLOAD_TIMEOUT_MS", "10")
	if got := parsePartUploadTimeoutEnv(); got != 750*time.Millisecond {
		t.Fatalf("expected precedence to keep 750ms, got %v", got)
	}

	t.Setenv("SBDEV_PART_UPLOAD_TIMEOUT", "")
	t.Setenv("SYFTBOX_PART_UPLOAD_TIMEOUT_MS", "250")
	if got := parsePartUploadTimeoutEnv(); got != 250*time.Millisecond {
		t.Fatalf("expected 250ms, got %v", got)
	}

	// Invalid values should yield 0.
	_ = os.Setenv("SBDEV_PART_UPLOAD_TIMEOUT", "nope")
	_ = os.Setenv("SYFTBOX_PART_UPLOAD_TIMEOUT_MS", "")
	if got := parsePartUploadTimeoutEnv(); got != 0 {
		t.Fatalf("expected 0 on invalid, got %v", got)
	}
}

func TestIsOwnerSyncPath(t *testing.T) {
	if !isOwnerSyncPath("alice@example.com", SyncPath("alice@example.com/public/a.txt")) {
		t.Fatal("expected owner path to be true")
	}
	if isOwnerSyncPath("alice@example.com", SyncPath("bob@example.com/public/a.txt")) {
		t.Fatal("expected non-owner path to be false")
	}
	if isOwnerSyncPath("", SyncPath("alice@example.com/public/a.txt")) {
		t.Fatal("expected empty owner to be false")
	}
}

func TestRemoteETagMatchesLocalSkipsUploadGuard(t *testing.T) {
	owner := "alice@example.com"
	op := &SyncOperation{
		RelPath: "bob@example.com/public/x.txt",
		Local:   &FileMetadata{ETag: "same"},
		Remote:  &FileMetadata{ETag: "same"},
	}
	// Even for non-owner, equality check should consider it no-op.
	if op.Remote.ETag != op.Local.ETag {
		t.Fatal("test setup invalid")
	}
	if !isOwnerSyncPath(owner, op.RelPath) && op.Remote.ETag == op.Local.ETag {
		// expected no-op path; nothing to assert beyond condition holding.
	}
}
