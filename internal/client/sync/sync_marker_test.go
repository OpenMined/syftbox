package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetMarker_Rejected_DedupesWithoutRotation(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "file.h5ad")
	if err := os.WriteFile(orig, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := SetMarker(orig, Rejected)
	if err != nil {
		t.Fatal(err)
	}
	if !RejectedFileExists(first) {
		t.Fatalf("expected rejected marker to exist: %s", first)
	}

	// Recreate original file (simulating app rewriting) and mark again.
	if err := os.WriteFile(orig, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := SetMarker(orig, Rejected)
	if err != nil {
		t.Fatal(err)
	}

	// Should keep the original marker path and not create rotated copies.
	if second != first {
		t.Fatalf("expected second marker to reuse first: %s vs %s", second, first)
	}
	// Original file should be removed.
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Fatalf("expected original file removed after duplicate rejection")
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "file.rejected*"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 rejected marker file, got %d: %v", len(matches), matches)
	}
}

