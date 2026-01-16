//go:build integration
// +build integration

package main

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
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
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(path)
			if err == nil {
				if fmt.Sprintf("%x", md5.Sum(data)) == wantMD5 {
					return nil
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("timeout waiting for %s", relPath)
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
		expectPropagation(owner, publicRel, md5Perm, windowsTimeout(20*time.Second))

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
		expectPropagation(owner, publicRel, md5Shared, windowsTimeout(20*time.Second))

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
		expectPropagation(owner, publicRel, md5Restrict, windowsTimeout(20*time.Second))
	}

	// RPC ACL: update and verify propagation
	app := "aclprop"
	endpoint := "rpc1"
	if err := h.bob.SetupRPCEndpoint(app, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}
	rpcRootRel := filepath.Join("app_data", app, "rpc", "syft.pub.yaml")
	rpcRel := filepath.Join("app_data", app, "rpc", endpoint, "syft.pub.yaml")
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
	expectPropagation(h.bob, rpcRootRel, md5RPCRoot, windowsTimeout(30*time.Second))
	expectPropagation(h.bob, rpcRel, md5RPC1, windowsTimeout(30*time.Second))

	md5RPC2, err := writeACL(h.bob, rpcRel, rpcACL2)
	if err != nil {
		t.Fatalf("write rpc acl2: %v", err)
	}
	expectPropagation(h.bob, rpcRel, md5RPC2, windowsTimeout(30*time.Second))
}
