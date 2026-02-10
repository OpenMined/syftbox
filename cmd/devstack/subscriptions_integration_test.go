//go:build integration
// +build integration

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSubConfig(t *testing.T, dataDir string, content []byte) {
	t.Helper()
	subPath := filepath.Join(dataDir, ".data", "syft.sub.yaml")
	if err := os.MkdirAll(filepath.Dir(subPath), 0o755); err != nil {
		t.Fatalf("create sub dir: %v", err)
	}
	if err := os.WriteFile(subPath, content, 0o600); err != nil {
		t.Fatalf("write syft.sub.yaml: %v", err)
	}
}

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

	subContent := []byte(`version: 1
defaults:
  action: block
rules:
  - action: allow
    datasite: "bob@example.com"
    path: "**"
`)
	writeSubConfig(t, h.bob.dataDir, subContent)

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

// TestSubscriptionsPauseKeepsLocal verifies that pause keeps local data and stops new syncs.
func TestSubscriptionsPauseKeepsLocal(t *testing.T) {
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

	allowContent := []byte(`version: 1
defaults:
  action: block
rules:
  - action: allow
    datasite: "alice@example.com"
    path: "public/**"
`)
	writeSubConfig(t, h.bob.dataDir, allowContent)
	time.Sleep(2 * time.Second)

	keepPath := "public/paused/keep.txt"
	keepPayload := []byte("keep me")
	keepMD5 := CalculateMD5(keepPayload)
	if err := h.alice.UploadFile(keepPath, keepPayload); err != nil {
		t.Fatalf("alice upload keep: %v", err)
	}
	if err := h.bob.WaitForFile(h.alice.email, keepPath, keepMD5, windowsTimeout(10*time.Second)); err != nil {
		t.Fatalf("expected bob to receive keep file: %v", err)
	}

	pauseContent := []byte(`version: 1
defaults:
  action: block
rules:
  - action: allow
    datasite: "alice@example.com"
    path: "public/**"
  - action: pause
    datasite: "alice@example.com"
    path: "public/paused/**"
`)
	writeSubConfig(t, h.bob.dataDir, pauseContent)
	time.Sleep(2 * time.Second)

	// Existing file should still be present.
	if err := h.bob.WaitForFile(h.alice.email, keepPath, keepMD5, windowsTimeout(5*time.Second)); err != nil {
		t.Fatalf("expected bob to keep paused file: %v", err)
	}

	// New file under paused path should not sync.
	newPath := "public/paused/new.txt"
	newPayload := []byte("paused new")
	if err := h.alice.UploadFile(newPath, newPayload); err != nil {
		t.Fatalf("alice upload new paused: %v", err)
	}
	if err := h.bob.WaitForFile(h.alice.email, newPath, CalculateMD5(newPayload), windowsTimeout(5*time.Second)); err == nil {
		t.Fatalf("expected bob to NOT receive paused file %s", newPath)
	}

	// File outside paused path should still sync.
	okPath := "public/ok.txt"
	okPayload := []byte("ok")
	if err := h.alice.UploadFile(okPath, okPayload); err != nil {
		t.Fatalf("alice upload ok: %v", err)
	}
	if err := h.bob.WaitForFile(h.alice.email, okPath, CalculateMD5(okPayload), windowsTimeout(10*time.Second)); err != nil {
		t.Fatalf("expected bob to receive ok file: %v", err)
	}
}

// TestSubscriptionsDatasiteGlob verifies datasite glob matching.
func TestSubscriptionsDatasiteGlob(t *testing.T) {
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

	subContent := []byte(`version: 1
defaults:
  action: block
rules:
  - action: allow
    datasite: "*@example.com"
    path: "public/**"
`)
	writeSubConfig(t, h.bob.dataDir, subContent)
	time.Sleep(2 * time.Second)

	filename := "public/glob-allow.txt"
	payload := []byte("glob allow")
	md5Hash := CalculateMD5(payload)
	if err := h.alice.UploadFile(filename, payload); err != nil {
		t.Fatalf("alice upload failed: %v", err)
	}
	if err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, windowsTimeout(10*time.Second)); err != nil {
		t.Fatalf("expected bob to receive file %s via glob rule: %v", filename, err)
	}
}

// TestSubscriptionsLastMatchWins verifies overlapping rules use last match.
func TestSubscriptionsLastMatchWins(t *testing.T) {
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

	subContent := []byte(`version: 1
defaults:
  action: block
rules:
  - action: allow
    datasite: "alice@example.com"
    path: "public/**"
  - action: block
    datasite: "alice@example.com"
    path: "public/tmp/**"
  - action: allow
    datasite: "alice@example.com"
    path: "public/tmp/keep/**"
`)
	writeSubConfig(t, h.bob.dataDir, subContent)
	time.Sleep(2 * time.Second)

	allowedPath := "public/tmp/keep/ok.txt"
	allowedPayload := []byte("allow")
	if err := h.alice.UploadFile(allowedPath, allowedPayload); err != nil {
		t.Fatalf("alice upload allow failed: %v", err)
	}
	if err := h.bob.WaitForFile(h.alice.email, allowedPath, CalculateMD5(allowedPayload), windowsTimeout(10*time.Second)); err != nil {
		t.Fatalf("expected bob to receive allowed file: %v", err)
	}

	blockedPath := "public/tmp/nope.txt"
	blockedPayload := []byte("block")
	if err := h.alice.UploadFile(blockedPath, blockedPayload); err != nil {
		t.Fatalf("alice upload block failed: %v", err)
	}
	if err := h.bob.WaitForFile(h.alice.email, blockedPath, CalculateMD5(blockedPayload), windowsTimeout(5*time.Second)); err == nil {
		t.Fatalf("expected bob to NOT receive blocked file %s", blockedPath)
	}
}
