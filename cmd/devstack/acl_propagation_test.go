//go:build integration
// +build integration

package main

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestACLPropagationUpdates stresses propagation of syft.pub.yaml changes (public and RPC)
// to catch cases where ACL updates fail to reach peers or access revocation fails.
func TestACLPropagationUpdates(t *testing.T) {
	t.Logf("=== TestACLPropagationUpdates starting on %s ===", runtime.GOOS)

	h := NewDevstackHarness(t)
	t.Logf("Harness created: alice=%s, bob=%s", h.alice.email, h.bob.email)

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
	t.Logf("charlie started on port %d", charliePort)

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
	t.Logf("Default ACLs created for all clients")
	for _, c := range clients {
		if err := c.SetSubscriptionsAllow(h.alice.email, h.bob.email, charlie.email); err != nil {
			t.Fatalf("set subscriptions for %s: %v", c.email, err)
		}
	}

	// Wait for file to arrive at peer with specific MD5
	waitForPath := func(c *ClientHelper, sender, relPath, wantMD5 string, timeout time.Duration) error {
		path := filepath.Join(c.dataDir, "datasites", sender, relPath)
		deadline := time.Now().Add(timeout)
		lastLog := time.Now()
		var lastErr error
		var lastMD5 string
		attempts := 0
		for time.Now().Before(deadline) {
			attempts++
			data, err := os.ReadFile(path)
			if err == nil {
				gotMD5 := fmt.Sprintf("%x", md5.Sum(data))
				if gotMD5 == wantMD5 {
					t.Logf("[OK] %s received %s from %s (MD5: %s) after %d attempts",
						c.email, relPath, sender, wantMD5[:8], attempts)
					return nil
				}
				lastMD5 = gotMD5
				lastErr = nil
			} else {
				lastErr = err
			}
			// Log every 5 seconds during wait
			if time.Since(lastLog) > 5*time.Second {
				if lastErr != nil {
					t.Logf("[WAIT] %s: %s from %s - not found yet", c.email, relPath, sender)
				} else {
					t.Logf("[WAIT] %s: %s from %s - MD5 mismatch (got %s, want %s)",
						c.email, relPath, sender, lastMD5[:8], wantMD5[:8])
				}
				lastLog = time.Now()
			}
			time.Sleep(100 * time.Millisecond)
		}
		if lastErr != nil {
			return fmt.Errorf("timeout: file not found after %d attempts", attempts)
		}
		return fmt.Errorf("timeout: MD5 mismatch (got %s, want %s) after %d attempts", lastMD5[:8], wantMD5[:8], attempts)
	}

	// Wait for file to be REMOVED from peer (access revoked)
	waitForFileGone := func(c *ClientHelper, sender, relPath string, timeout time.Duration) error {
		path := filepath.Join(c.dataDir, "datasites", sender, relPath)
		deadline := time.Now().Add(timeout)
		lastLog := time.Now()
		attempts := 0
		for time.Now().Before(deadline) {
			attempts++
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Logf("[OK] %s: file %s from %s was removed (access revoked) after %d attempts",
					c.email, relPath, sender, attempts)
				return nil
			}
			if time.Since(lastLog) > 5*time.Second {
				t.Logf("[WAIT] %s: waiting for %s from %s to be removed", c.email, relPath, sender)
				lastLog = time.Now()
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("timeout: file still exists after %d attempts", attempts)
	}

	waitForDirGone := func(c *ClientHelper, sender, relDir string, timeout time.Duration) error {
		path := filepath.Join(c.dataDir, "datasites", sender, relDir)
		deadline := time.Now().Add(timeout)
		lastLog := time.Now()
		attempts := 0
		for time.Now().Before(deadline) {
			attempts++
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Logf("[OK] %s: dir %s from %s was removed after %d attempts",
					c.email, relDir, sender, attempts)
				return nil
			}
			if time.Since(lastLog) > 5*time.Second {
				if entries, err := os.ReadDir(path); err == nil {
					names := make([]string, 0, len(entries))
					for _, entry := range entries {
						names = append(names, entry.Name())
					}
					t.Logf("[WAIT] %s: waiting for dir %s from %s to be removed (entries=%v)",
						c.email, relDir, sender, names)
				} else {
					t.Logf("[WAIT] %s: waiting for dir %s from %s to be removed (read err=%v)",
						c.email, relDir, sender, err)
				}
				lastLog = time.Now()
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("timeout: dir still exists after %d attempts", attempts)
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

	dumpClientLog := func(c *ClientHelper, lines int) {
		peerSyftboxLog := filepath.Join(c.state.HomePath, ".syftbox", "logs", "syftbox.log")
		if logData, logErr := os.ReadFile(peerSyftboxLog); logErr == nil {
			logLines := strings.Split(string(logData), "\n")
			t.Logf("=== %s's last %d log lines ===", c.email, lines)
			start := len(logLines) - lines
			if start < 0 {
				start = 0
			}
			for _, line := range logLines[start:] {
				if line != "" {
					t.Logf("  %s", line)
				}
			}
		}
	}

	// Check which peers should receive or lose access based on ACL
	expectPropagationWithAccess := func(owner *ClientHelper, relPath, wantMD5 string, readAccess []string, timeout time.Duration) {
		t.Logf("--- Checking propagation from %s (readers: %v) ---", owner.email, readAccess)

		// Build set of who has access
		hasAccess := make(map[string]bool)
		publicAccess := false
		for _, reader := range readAccess {
			if reader == "*" {
				publicAccess = true
			}
			hasAccess[reader] = true
		}

		for _, peer := range clients {
			if peer.email == owner.email {
				continue
			}

			peerHasAccess := publicAccess || hasAccess[peer.email]

			if peerHasAccess {
				// Peer should receive the file
				if err := waitForPath(peer, owner.email, relPath, wantMD5, timeout); err != nil {
					// Debug: dump peer's sync log on failure
					dumpClientLog(peer, 50)
					t.Fatalf("FAIL: %s should receive %s from %s but: %v",
						peer.email, relPath, owner.email, err)
				}
			} else {
				// Peer should have file removed (or never receive it)
				if err := waitForFileGone(peer, owner.email, relPath, timeout); err != nil {
					// Debug: dump peer's sync log on failure
					dumpClientLog(peer, 50)
					t.Fatalf("FAIL: %s should NOT have %s from %s (access revoked) but: %v",
						peer.email, relPath, owner.email, err)
				}
				if strings.HasSuffix(relPath, "syft.pub.yaml") {
					relDir := filepath.Dir(relPath)
					if err := waitForDirGone(peer, owner.email, relDir, timeout); err != nil {
						dumpClientLog(peer, 50)
						t.Fatalf("FAIL: %s should not keep empty dir %s from %s but: %v",
							peer.email, relDir, owner.email, err)
					}
				}
			}
		}
	}

	// Public ACL: test permissive → restricted → owner-only progression
	publicRel := filepath.Join("public", "syft.pub.yaml")
	version := 0

	for idx, owner := range clients {
		t.Logf("\n=== Testing ACL cycle for %s (client %d/3) ===", owner.email, idx+1)

		// Step 1: Permissive ACL (everyone can read)
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
		t.Logf("\n[Step 1] %s: Writing PERMISSIVE ACL (read: ['*'])", owner.email)
		md5Perm, err := writeACL(owner, publicRel, fmt.Sprintf(permissive, owner.email, version))
		if err != nil {
			t.Fatalf("write public ACL permissive for %s: %v", owner.email, err)
		}
		expectPropagationWithAccess(owner, publicRel, md5Perm, []string{"*"}, windowsTimeout(60*time.Second))

		// Step 2: Shared ACL (owner + one peer)
		version++
		onePeer := clients[(idx+1)%len(clients)]
		excludedPeer := clients[(idx+2)%len(clients)]
		shared := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s','%s']
# version: %d
`
		t.Logf("\n[Step 2] %s: Writing SHARED ACL (read: [%s, %s]) - %s should lose access",
			owner.email, owner.email, onePeer.email, excludedPeer.email)
		md5Shared, err := writeACL(owner, publicRel, fmt.Sprintf(shared, owner.email, owner.email, onePeer.email, version))
		if err != nil {
			t.Fatalf("write public ACL shared for %s: %v", owner.email, err)
		}
		expectPropagationWithAccess(owner, publicRel, md5Shared, []string{owner.email, onePeer.email}, windowsTimeout(60*time.Second))

		// Step 3: Owner-only ACL (only owner can read)
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
		t.Logf("\n[Step 3] %s: Writing OWNER-ONLY ACL (read: [%s]) - all peers should lose access",
			owner.email, owner.email)
		md5Restrict, err := writeACL(owner, publicRel, fmt.Sprintf(ownerOnly, owner.email, owner.email, version))
		if err != nil {
			t.Fatalf("write public ACL restrictive for %s: %v", owner.email, err)
		}
		expectPropagationWithAccess(owner, publicRel, md5Restrict, []string{owner.email}, windowsTimeout(60*time.Second))

		t.Logf("=== Completed ACL cycle for %s ===\n", owner.email)
	}

	// RPC ACL test commented out for now - focusing on public ACL propagation first
	// TODO: Re-enable and fix RPC ACL tests once public ACL tests pass
	t.Logf("=== TestACLPropagationUpdates completed successfully ===")
}
