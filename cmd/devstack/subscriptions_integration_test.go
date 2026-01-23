//go:build integration
// +build integration

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSubscriptionsBlockRemote verifies that syft.sub.yaml blocks remote sync.
func TestSubscriptionsBlockRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := NewDevstackHarness(t)

	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	subPath := filepath.Join(h.bob.dataDir, ".data", "syft.sub.yaml")
	subContent := []byte(`version: 1
defaults:
  action: block
rules:
  - action: allow
    datasite: "bob@example.com"
    path: "**"
`)
	if err := os.MkdirAll(filepath.Dir(subPath), 0o755); err != nil {
		t.Fatalf("create sub dir: %v", err)
	}
	if err := os.WriteFile(subPath, subContent, 0o600); err != nil {
		t.Fatalf("write syft.sub.yaml: %v", err)
	}

	time.Sleep(2 * time.Second)

	payload := []byte("blocked-by-subscription")
	md5Hash := CalculateMD5(payload)
	filename := "sub-blocked.txt"
	if err := h.alice.UploadFile(filename, payload); err != nil {
		t.Fatalf("alice upload failed: %v", err)
	}

	err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, windowsTimeout(5*time.Second))
	if err == nil {
		t.Fatalf("expected bob to NOT receive file %s due to subscription block", filename)
	}
}
