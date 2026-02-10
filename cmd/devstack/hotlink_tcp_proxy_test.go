//go:build integration
// +build integration

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHotlinkTCPProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	for _, mode := range []string{"rust", "go"} {
		t.Run(mode, func(t *testing.T) {
			runHotlinkTCPProxyTest(t, mode)
		})
	}
}

func runHotlinkTCPProxyTest(t *testing.T, clientMode string) {
	t.Helper()

	t.Setenv("SYFTBOX_HOTLINK", "1")
	t.Setenv("SYFTBOX_HOTLINK_TCP_PROXY", "1")
	t.Setenv("SYFTBOX_HOTLINK_QUIC", "1")
	t.Setenv("SYFTBOX_HOTLINK_DEBUG", "1")
	t.Setenv("SYFTBOX_PRIORITY_DEBOUNCE_MS", "0")
	t.Setenv("SBDEV_CLIENT_MODE", clientMode)

	h := NewDevstackHarness(t)
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob ACLs: %v", err)
	}
	if err := h.AllowSubscriptionsBetween(h.alice, h.bob); err != nil {
		t.Fatalf("set subscriptions: %v", err)
	}

	alicePort := findFreePort(t)
	bobPort := findFreePort(t)
	t.Logf("TCP proxy ports: alice=%d bob=%d", alicePort, bobPort)

	mpcRelPath := filepath.Join("shared", "flows", "tcp-test", "run1", "_mpc", "0_to_1")

	createTCPProxyMarkers(t, h, mpcRelPath, alicePort, bobPort)

	// Wait for ACLs to sync to server so hotlink routing works.
	// The root ACL is private, so the MPC directory ACL must be synced.
	t.Log("Waiting for ACL sync...")
	time.Sleep(5 * time.Second)

	aliceAddr := fmt.Sprintf("127.0.0.1:%d", alicePort)
	bobAddr := fmt.Sprintf("127.0.0.1:%d", bobPort)

	// Connect directly with retries (no probe connections that race with clearTCPWriter)
	bobConn, err := dialWithRetry(bobAddr, 30*time.Second)
	if err != nil {
		t.Fatalf("connect to bob proxy: %v", err)
	}
	defer bobConn.Close()

	time.Sleep(500 * time.Millisecond)

	aliceConn, err := dialWithRetry(aliceAddr, 30*time.Second)
	if err != nil {
		t.Fatalf("connect to alice proxy: %v", err)
	}
	defer aliceConn.Close()

	t.Log("TCP proxy connections established")

	const numChunks = 20
	const chunkSize = 4096

	recvDone := make(chan []byte, 1)
	recvErr := make(chan error, 1)
	go func() {
		buf := make([]byte, numChunks*chunkSize)
		total := 0
		bobConn.SetReadDeadline(time.Now().Add(60 * time.Second))
		for total < numChunks*chunkSize {
			n, err := bobConn.Read(buf[total:])
			if n > 0 {
				total += n
			}
			if err != nil {
				if err == io.EOF && total == numChunks*chunkSize {
					break
				}
				recvErr <- fmt.Errorf("read at %d/%d: %w", total, numChunks*chunkSize, err)
				return
			}
		}
		recvDone <- buf[:total]
	}()

	for i := 0; i < numChunks; i++ {
		chunk := make([]byte, chunkSize)
		binary.BigEndian.PutUint32(chunk[0:4], uint32(i))
		for j := 4; j < chunkSize; j++ {
			chunk[j] = byte(i & 0xFF)
		}
		if _, err := aliceConn.Write(chunk); err != nil {
			t.Fatalf("write chunk %d: %v", i, err)
		}
	}
	t.Log("All chunks sent, waiting for receive...")

	select {
	case received := <-recvDone:
		if len(received) != numChunks*chunkSize {
			t.Fatalf("received %d bytes, expected %d", len(received), numChunks*chunkSize)
		}
		for i := 0; i < numChunks; i++ {
			offset := i * chunkSize
			chunkIdx := binary.BigEndian.Uint32(received[offset : offset+4])
			if chunkIdx != uint32(i) {
				t.Fatalf("chunk %d: expected index %d, got %d (out of order)", i, i, chunkIdx)
			}
			for j := 4; j < chunkSize; j++ {
				if received[offset+j] != byte(i&0xFF) {
					t.Fatalf("chunk %d byte %d: expected %d, got %d", i, j, byte(i&0xFF), received[offset+j])
				}
			}
		}
		t.Logf("All %d chunks received in correct order (%d bytes)", numChunks, len(received))

	case err := <-recvErr:
		t.Fatalf("receive error: %v", err)

	case <-time.After(60 * time.Second):
		t.Fatal("timeout waiting for data")
	}
}

func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func createTCPProxyMarkers(t *testing.T, h *DevstackTestHarness, mpcRelPath string, alicePort, bobPort int) {
	t.Helper()

	marker := map[string]interface{}{
		"from": h.alice.email,
		"to":   h.bob.email,
		"port": 0,
		"ports": map[string]int{
			h.alice.email: alicePort,
			h.bob.email:   bobPort,
		},
	}
	markerJSON, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("marshal marker: %v", err)
	}

	publicACL := fmt.Sprintf(`terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s', '%s']
      read: ['%s', '%s']
`, h.alice.email, h.bob.email, h.alice.email, h.bob.email)

	for _, client := range []*ClientHelper{h.alice, h.bob} {
		for _, dsOwner := range []string{h.alice.email, h.bob.email} {
			mpcDir := filepath.Join(client.dataDir, "datasites", dsOwner, mpcRelPath)
			if err := os.MkdirAll(mpcDir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", mpcDir, err)
			}

			markerPath := filepath.Join(mpcDir, "stream.tcp")
			if err := os.WriteFile(markerPath, markerJSON, 0o644); err != nil {
				t.Fatalf("write marker %s: %v", markerPath, err)
			}

			aclPath := filepath.Join(mpcDir, "syft.pub.yaml")
			if err := os.WriteFile(aclPath, []byte(publicACL), 0o644); err != nil {
				t.Fatalf("write ACL %s: %v", aclPath, err)
			}

			flowDir := filepath.Dir(filepath.Dir(filepath.Dir(mpcDir)))
			flowACLPath := filepath.Join(flowDir, "syft.pub.yaml")
			if err := os.WriteFile(flowACLPath, []byte(publicACL), 0o644); err != nil {
				t.Fatalf("write flow ACL %s: %v", flowACLPath, err)
			}
		}
	}
	t.Logf("Created TCP proxy markers and ACLs in both clients' datasites")
}

func dialWithRetry(addr string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err == nil {
			return conn, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, fmt.Errorf("timeout connecting to %s", addr)
}
