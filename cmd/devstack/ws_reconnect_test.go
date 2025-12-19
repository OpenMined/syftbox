//go:build integration
// +build integration

package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func waitForServerDown(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			return nil
		}
		_ = resp.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server still responding after %s", timeout)
}

// TestWebSocketReconnectAfterServerRestart ensures the clients reconnect their event socket when
// the server is restarted, and that priority RPC requests are delivered quickly after recovery.
func TestWebSocketReconnectAfterServerRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := NewDevstackHarness(t)

	appName := "wstest"
	endpoint := "reconnect"

	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("alice setup rpc: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("bob setup rpc: %v", err)
	}

	// Wait for ACLs to land on the server, then let the adaptive sync scheduler settle into its idle
	// interval so periodic sync is unlikely to race the websocket path.
	time.Sleep(3 * time.Second)
	time.Sleep(6 * time.Second)

	// Kill server (drops websocket connections).
	t.Log("Stopping server (simulate WS disconnect)...")
	_ = killProcess(h.state.Server.PID)
	if err := waitForServerDown(h.state.Server.Port, 5*time.Second); err != nil {
		t.Fatalf("server did not stop: %v", err)
	}

	relayRoot := filepath.Join(h.root, relayDir)
	t.Logf("Restarting server on same port %d...", h.state.Server.Port)
	sState, err := startServer(h.state.Server.BinPath, relayRoot, h.state.Server.Port, h.state.Minio.APIPort)
	if err != nil {
		t.Fatalf("restart server: %v", err)
	}
	h.state.Server = sState
	// Update state files so cleanup uses the restarted PID.
	_ = writeState(filepath.Join(relayRoot, stateFileName), h.state)
	_ = saveGlobalState(h.root, h.state)

	if err := getWithRetry(fmt.Sprintf("http://127.0.0.1:%d/healthz", h.state.Server.Port), 15*time.Second); err != nil {
		t.Fatalf("server not healthy after restart: %v", err)
	}

	// Give clients time to reconnect and settle back into idle.
	time.Sleep(6 * time.Second)

	// Write a priority file after restart; with WS priority sync, this should complete well under 1s.
	content := []byte("ws reconnect test")
	md5Hash := CalculateMD5(content)
	filename := "ws-reconnect.request"
	if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
		t.Fatalf("alice write request after restart: %v", err)
	}

	start := time.Now()
	if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, 1*time.Second); err != nil {
		t.Fatalf("bob did not receive request quickly after server restart: %v", err)
	}
	t.Logf("✅ WS reconnect delivery latency: %s", time.Since(start))
}
