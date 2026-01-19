//go:build integration
// +build integration

package main

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestACLPropagationUpdates stresses propagation of syft.pub.yaml changes (public and RPC)
// to catch cases where ACL updates fail to reach peers.
func TestACLPropagationUpdates(t *testing.T) {
	h := NewDevstackHarness(t)

	// Start a third client to mirror the chaos test topology.
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", h.state.Server.Port)
	charliePort, _ := getFreePort()
	charlieState, err := startClient(
		h.state.Clients[0].BinPath,
		h.root,
		"charlie@example.com",
		serverURL,
		charliePort,
	)
	if err != nil {
		t.Fatalf("start charlie: %v", err)
	}
	defer func() { _ = killProcess(charlieState.PID) }()

	charlie := &ClientHelper{
		t:         t,
		email:     charlieState.Email,
		state:     charlieState,
		dataDir:   charlieState.DataPath,
		publicDir: filepath.Join(charlieState.DataPath, "datasites", charlieState.Email, "public"),
		metrics:   &ClientMetrics{},
	}

	clients := []*ClientHelper{h.alice, h.bob, charlie}

	// Ensure defaults exist
	for _, c := range clients {
		if err := c.CreateDefaultACLs(); err != nil {
			t.Fatalf("default ACLs for %s: %v", c.email, err)
		}
	}

	waitForPath := func(c *ClientHelper, sender, relPath, wantMD5 string, timeout time.Duration) error {
		path := filepath.Join(c.dataDir, "datasites", sender, relPath)
		deadline := time.Now().Add(timeout)
		lastLog := time.Now()
		var lastErr error
		var lastMD5 string
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(path)
			if err == nil {
				gotMD5 := fmt.Sprintf("%x", md5.Sum(data))
				if gotMD5 == wantMD5 {
					return nil
				}
				lastMD5 = gotMD5
				lastErr = nil
			} else {
				lastErr = err
			}
			// Log every 10 seconds during wait
			if time.Since(lastLog) > 10*time.Second {
				if lastErr != nil {
					t.Logf("DEBUG waitForPath: %s waiting for %s from %s - file not found: %v", c.email, relPath, sender, lastErr)
				} else {
					t.Logf("DEBUG waitForPath: %s waiting for %s from %s - MD5 mismatch: got %s, want %s", c.email, relPath, sender, lastMD5, wantMD5)
				}
				// Also check parent directory
				parentDir := filepath.Dir(path)
				if entries, err := os.ReadDir(parentDir); err == nil {
					t.Logf("DEBUG waitForPath: parent dir %s contains:", parentDir)
					for _, e := range entries {
						t.Logf("  - %s", e.Name())
					}
				} else {
					t.Logf("DEBUG waitForPath: parent dir %s does not exist: %v", parentDir, err)
				}
				lastLog = time.Now()
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Final debug on timeout
		if lastErr != nil {
			return fmt.Errorf("timeout waiting for %s (last error: %v)", relPath, lastErr)
		}
		return fmt.Errorf("timeout waiting for %s (MD5 mismatch: got %s, want %s)", relPath, lastMD5, wantMD5)
	}

	writeACL := func(c *ClientHelper, relPath, content string) (string, error) {
		aclPath := filepath.Join(c.dataDir, "datasites", c.email, relPath)
		if err := os.MkdirAll(filepath.Dir(aclPath), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(aclPath, []byte(content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("%x", md5.Sum([]byte(content))), nil
	}

	expectPropagation := func(owner *ClientHelper, relPath, wantMD5 string, timeout time.Duration) {
		for _, peer := range clients {
			if peer.email == owner.email {
				continue
			}
			if err := waitForPath(peer, owner.email, relPath, wantMD5, timeout); err != nil {
				// Debug: dump peer's sync log on failure
				peerSyftboxLog := filepath.Join(peer.state.HomePath, ".syftbox", "logs", "syftbox.log")
				if logData, logErr := os.ReadFile(peerSyftboxLog); logErr == nil {
					lines := strings.Split(string(logData), "\n")
					t.Logf("DEBUG FAILURE: %s's last 30 log lines:", peer.email)
					start := len(lines) - 30
					if start < 0 {
						start = 0
					}
					for _, line := range lines[start:] {
						t.Logf("  %s", line)
					}
				}
				t.Fatalf("propagation of %s from %s to %s failed: %v", relPath, owner.email, peer.email, err)
			}
		}
	}

	// Public ACL: flip between permissive, single-peer, and owner-only for each participant.
	publicRel := filepath.Join("public", "syft.pub.yaml")
	version := 0
	for idx, owner := range clients {
		version++
		permissive := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['*']
# version: %d
`
		md5Perm, err := writeACL(owner, publicRel, fmt.Sprintf(permissive, owner.email, version))
		if err != nil {
			t.Fatalf("write public ACL permissive for %s: %v", owner.email, err)
		}
		expectPropagation(owner, publicRel, md5Perm, windowsTimeout(30*time.Second))

		version++
		onePeer := clients[(idx+1)%len(clients)]
		shared := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s','%s']
# version: %d
`
		md5Shared, err := writeACL(owner, publicRel, fmt.Sprintf(shared, owner.email, owner.email, onePeer.email, version))
		if err != nil {
			t.Fatalf("write public ACL shared for %s: %v", owner.email, err)
		}
		expectPropagation(owner, publicRel, md5Shared, windowsTimeout(30*time.Second))

		version++
		ownerOnly := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s']
# version: %d
`
		md5Restrict, err := writeACL(owner, publicRel, fmt.Sprintf(ownerOnly, owner.email, owner.email, version))
		if err != nil {
			t.Fatalf("write public ACL restrictive for %s: %v", owner.email, err)
		}
		expectPropagation(owner, publicRel, md5Restrict, windowsTimeout(30*time.Second))
	}

	// RPC ACL: update and verify propagation
	app := "aclprop"
	endpoint := "rpc1"

	t.Logf("DEBUG: Setting up RPC endpoint for bob")
	if err := h.bob.SetupRPCEndpoint(app, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}

	rpcRootRel := filepath.Join("app_data", app, "rpc", "syft.pub.yaml")
	rpcRel := filepath.Join("app_data", app, "rpc", endpoint, "syft.pub.yaml")

	// Debug: verify bob's files exist locally
	bobRootACLPath := filepath.Join(h.bob.dataDir, "datasites", h.bob.email, rpcRootRel)
	bobEndpointACLPath := filepath.Join(h.bob.dataDir, "datasites", h.bob.email, rpcRel)
	if _, err := os.Stat(bobRootACLPath); err != nil {
		t.Fatalf("DEBUG: bob's root ACL not created: %v", err)
	}
	t.Logf("DEBUG: bob's root ACL exists at %s", bobRootACLPath)
	if _, err := os.Stat(bobEndpointACLPath); err != nil {
		t.Fatalf("DEBUG: bob's endpoint ACL not created: %v", err)
	}
	t.Logf("DEBUG: bob's endpoint ACL exists at %s", bobEndpointACLPath)

	// Debug: check what datasites alice is subscribed to
	aliceDatasites := filepath.Join(h.alice.dataDir, "datasites")
	entries, _ := os.ReadDir(aliceDatasites)
	t.Logf("DEBUG: alice's datasites directory contains:")
	for _, e := range entries {
		t.Logf("  - %s", e.Name())
	}

	// Debug: trigger sync and wait briefly
	t.Logf("DEBUG: Triggering sync for bob and waiting for upload...")
	time.Sleep(2 * time.Second)

	// Debug: check bob's sync log for upload activity
	bobSyftboxLog := filepath.Join(h.bob.state.HomePath, ".syftbox", "logs", "syftbox.log")
	if logData, err := os.ReadFile(bobSyftboxLog); err == nil {
		lines := strings.Split(string(logData), "\n")
		t.Logf("DEBUG: bob's last 20 log lines:")
		start := len(lines) - 20
		if start < 0 {
			start = 0
		}
		for _, line := range lines[start:] {
			if strings.Contains(line, "upload") || strings.Contains(line, "sync") || strings.Contains(line, "aclprop") {
				t.Logf("  %s", line)
			}
		}
	} else {
		t.Logf("DEBUG: couldn't read bob's syftbox log: %v", err)
	}

	// Debug: check alice's view of bob's datasite
	aliceViewOfBob := filepath.Join(h.alice.dataDir, "datasites", h.bob.email)
	if _, err := os.Stat(aliceViewOfBob); err != nil {
		t.Logf("DEBUG: alice has NO view of bob's datasite yet: %v", err)
	} else {
		t.Logf("DEBUG: alice has view of bob's datasite at %s", aliceViewOfBob)
		bobEntries, _ := os.ReadDir(aliceViewOfBob)
		t.Logf("DEBUG: alice sees in bob's datasite:")
		for _, e := range bobEntries {
			t.Logf("  - %s", e.Name())
		}
	}

	// Debug: check charlie's view too
	charlieViewOfBob := filepath.Join(charlie.dataDir, "datasites", h.bob.email)
	if _, err := os.Stat(charlieViewOfBob); err != nil {
		t.Logf("DEBUG: charlie has NO view of bob's datasite yet: %v", err)
	} else {
		t.Logf("DEBUG: charlie has view of bob's datasite at %s", charlieViewOfBob)
	}

	rpcACL := `rules:
  - pattern: '**.request'
    access:
      admin: []
      read: ['*']
      write: ['alice@example.com','bob@example.com']
  - pattern: '**.response'
    access:
      admin: []
      read: ['alice@example.com','bob@example.com']
      write: ['alice@example.com','bob@example.com']
`
	rpcACL2 := `rules:
  - pattern: '**.request'
    access:
      admin: []
      read: ['alice@example.com']
      write: ['alice@example.com']
  - pattern: '**.response'
    access:
      admin: []
      read: ['alice@example.com']
      write: ['alice@example.com']
`

	md5RPC1, err := writeACL(h.bob, rpcRel, rpcACL)
	if err != nil {
		t.Fatalf("write rpc acl1: %v", err)
	}
	rootACLData, err := os.ReadFile(filepath.Join(h.bob.dataDir, "datasites", h.bob.email, rpcRootRel))
	if err != nil {
		t.Fatalf("read rpc root acl: %v", err)
	}
	md5RPCRoot := fmt.Sprintf("%x", md5.Sum(rootACLData))
	expectPropagation(h.bob, rpcRootRel, md5RPCRoot, windowsTimeout(60*time.Second))
	expectPropagation(h.bob, rpcRel, md5RPC1, windowsTimeout(60*time.Second))

	md5RPC2, err := writeACL(h.bob, rpcRel, rpcACL2)
	if err != nil {
		t.Fatalf("write rpc acl2: %v", err)
	}
	expectPropagation(h.bob, rpcRel, md5RPC2, windowsTimeout(60*time.Second))
}
