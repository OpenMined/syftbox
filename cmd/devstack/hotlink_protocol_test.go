//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHotlinkProtocolE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	t.Setenv("SYFTBOX_HOTLINK", "1")
	t.Setenv("SYFTBOX_HOTLINK_SOCKET_ONLY", "1")
	t.Setenv("SYFTBOX_PRIORITY_DEBOUNCE_MS", "0")
	t.Setenv("SYFTBOX_HOTLINK_DEBUG", "1")

	h := NewDevstackHarness(t)
	if err := h.StartProfiling("TestHotlinkProtocolE2E"); err != nil {
		t.Fatalf("start profiling: %v", err)
	}
	defer h.StopProfiling()

	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}
	if err := h.AllowSubscriptionsBetween(h.alice, h.bob); err != nil {
		t.Fatalf("set subscriptions: %v", err)
	}

	appName := "latency"
	endpoint := "hotlink-protocol"
	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}

	aclPath := filepath.Join(
		h.alice.dataDir,
		"datasites",
		h.alice.email,
		"app_data",
		appName,
		"rpc",
		endpoint,
		"syft.pub.yaml",
	)
	aclBytes, err := os.ReadFile(aclPath)
	if err != nil {
		t.Fatalf("read ACL file: %v", err)
	}
	aclMD5 := CalculateMD5(aclBytes)
	if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, "syft.pub.yaml", aclMD5, 10*time.Second); err != nil {
		t.Fatalf("wait for ACL sync: %v", err)
	}

	acceptPath := hotlinkAcceptPath(h, appName, endpoint)
	if err := os.MkdirAll(filepath.Dir(acceptPath), 0o755); err != nil {
		t.Fatalf("create accept dir: %v", err)
	}
	if err := os.WriteFile(acceptPath, []byte("1"), 0o644); err != nil {
		t.Fatalf("write accept marker: %v", err)
	}
	if err := writeHotlinkSenderMarker(h, appName, endpoint); err != nil {
		t.Fatalf("write sender marker: %v", err)
	}

	time.Sleep(1 * time.Second)

	warmupPayload := timedPayload(64)
	warmupPath := hotlinkRelPath(h.alice.email, appName, endpoint, "warmup.request")

	senderConn, err := dialHotlinkIPC(hotlinkSenderIPCPath(h, appName, endpoint), 10*time.Second)
	if err != nil {
		t.Fatalf("open sender ipc: %v", err)
	}
	defer senderConn.Close()

	if err := writeHotlinkFrame(senderConn, warmupPath, warmupPayload); err != nil {
		t.Fatalf("warmup send: %v", err)
	}

	receiverConn, err := dialHotlinkIPC(hotlinkIPCPath(h, appName, endpoint), 10*time.Second)
	if err != nil {
		t.Fatalf("open receiver ipc: %v", err)
	}
	defer receiverConn.Close()

	frameCh, errCh := startHotlinkFrameReader(receiverConn)
	if _, err := readHotlinkPayload(frameCh, errCh, "", 0, 10*time.Second); err != nil {
		t.Fatalf("warmup read: %v", err)
	}

	cases := []struct {
		name       string
		size       int
		iterations int
	}{
		{"1KB", 1024, 10},
		{"10KB", 10 * 1024, 10},
		{"100KB", 100 * 1024, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < tc.iterations; i++ {
				payload := timedPayload(tc.size)
				filename := fmt.Sprintf("proto-%s-%02d.request", tc.name, i)
				expectedPath := hotlinkRelPath(h.alice.email, appName, endpoint, filename)
				if err := writeHotlinkFrame(senderConn, expectedPath, payload); err != nil {
					t.Fatalf("send frame: %v", err)
				}
				latency, err := readHotlinkPayloadWithTimestamp(frameCh, errCh, expectedPath, tc.size, 10*time.Second)
				if err != nil {
					t.Fatalf("read frame: %v", err)
				}
				h.metrics.RecordLatency(latency)
			}
		})
	}

	h.metrics.GenerateReport().Log(t)

	// Edge case: sender reconnects.
	if err := senderConn.Close(); err == nil {
		senderConn, _ = dialHotlinkIPC(hotlinkSenderIPCPath(h, appName, endpoint), 10*time.Second)
		if senderConn == nil {
			t.Fatalf("reconnect sender ipc failed")
		}
		defer senderConn.Close()
		reconnectPayload := timedPayload(256)
		reconnectPath := hotlinkRelPath(h.alice.email, appName, endpoint, "reconnect.request")
		if err := writeHotlinkFrame(senderConn, reconnectPath, reconnectPayload); err != nil {
			t.Fatalf("reconnect send: %v", err)
		}
		if _, err := readHotlinkPayload(frameCh, errCh, reconnectPath, 256, 10*time.Second); err != nil {
			t.Fatalf("reconnect read: %v", err)
		}
	}

	// Edge case: delayed accept.
	runDelayedAcceptHotlinkCase(t, h, appName, "hotlink-delayed")

	// Edge case: no accept (expect timeout / no delivery).
	runNoAcceptHotlinkCase(t, h, appName, "hotlink-noaccept")

	_ = os.Remove(acceptPath)
	_ = os.Remove(hotlinkSenderIPCPath(h, appName, endpoint))
}

func hotlinkAcceptPath(h *DevstackTestHarness, appName, endpoint string) string {
	acceptDir := filepath.Dir(h.bob.rpcReceivePath(h.alice.email, appName, endpoint, "accept.tmp"))
	return filepath.Join(acceptDir, "stream.accept")
}

func writeHotlinkSenderMarker(h *DevstackTestHarness, appName, endpoint string) error {
	senderMarker := hotlinkSenderIPCPath(h, appName, endpoint)
	if err := os.MkdirAll(filepath.Dir(senderMarker), 0o755); err != nil {
		return err
	}
	return os.WriteFile(senderMarker, []byte(""), 0o644)
}

func runDelayedAcceptHotlinkCase(t *testing.T, h *DevstackTestHarness, appName, endpoint string) {
	t.Helper()

	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}
	if err := writeHotlinkSenderMarker(h, appName, endpoint); err != nil {
		t.Fatalf("write sender marker: %v", err)
	}

	payload := timedPayload(512)
	path := hotlinkRelPath(h.alice.email, appName, endpoint, "delayed.request")

	senderConn, err := dialHotlinkIPC(hotlinkSenderIPCPath(h, appName, endpoint), 10*time.Second)
	if err != nil {
		t.Fatalf("open delayed sender ipc: %v", err)
	}
	defer senderConn.Close()

	go func() {
		time.Sleep(200 * time.Millisecond)
		acceptPath := hotlinkAcceptPath(h, appName, endpoint)
		_ = os.MkdirAll(filepath.Dir(acceptPath), 0o755)
		_ = os.WriteFile(acceptPath, []byte("1"), 0o644)
	}()

	if err := writeHotlinkFrame(senderConn, path, payload); err != nil {
		t.Fatalf("delayed send: %v", err)
	}

	receiverConn, err := dialHotlinkIPC(hotlinkIPCPath(h, appName, endpoint), 10*time.Second)
	if err != nil {
		t.Fatalf("open delayed receiver ipc: %v", err)
	}
	defer receiverConn.Close()

	frameCh, errCh := startHotlinkFrameReader(receiverConn)
	if _, err := readHotlinkPayloadWithTimestamp(frameCh, errCh, path, len(payload), 10*time.Second); err != nil {
		t.Fatalf("delayed accept read: %v", err)
	}

	_ = os.Remove(hotlinkAcceptPath(h, appName, endpoint))
	_ = os.Remove(hotlinkSenderIPCPath(h, appName, endpoint))
}

func runNoAcceptHotlinkCase(t *testing.T, h *DevstackTestHarness, appName, endpoint string) {
	t.Helper()

	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}
	if err := writeHotlinkSenderMarker(h, appName, endpoint); err != nil {
		t.Fatalf("write sender marker: %v", err)
	}

	payload := timedPayload(512)
	path := hotlinkRelPath(h.alice.email, appName, endpoint, "noaccept.request")

	senderConn, err := dialHotlinkIPC(hotlinkSenderIPCPath(h, appName, endpoint), 5*time.Second)
	if err != nil {
		t.Fatalf("open noaccept sender ipc: %v", err)
	}
	defer senderConn.Close()

	if err := writeHotlinkFrame(senderConn, path, payload); err != nil {
		t.Fatalf("noaccept send: %v", err)
	}

	receiverConn, err := dialHotlinkIPC(hotlinkIPCPath(h, appName, endpoint), 1*time.Second)
	if err == nil {
		defer receiverConn.Close()
		frameCh, errCh := startHotlinkFrameReader(receiverConn)
		if _, err := readHotlinkPayload(frameCh, errCh, path, len(payload), 1*time.Second); err == nil {
			t.Fatalf("expected noaccept timeout")
		}
	}

	_ = os.Remove(hotlinkSenderIPCPath(h, appName, endpoint))
}
