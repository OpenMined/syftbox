//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestDevstackIntegration tests the full devstack lifecycle:
// 1. Start devstack with MinIO + server + multiple clients
// 2. Verify files sync between clients
// 3. Stop devstack cleanly
func TestDevstackIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping devstack integration on Windows runner")
	}

	// Setup test directory
	tmpDir := t.TempDir()
	stackRoot := filepath.Join(tmpDir, "teststack")

	// Test parameters
	emails := []string{"alice@example.com", "bob@example.com"}

	// Parse start options
	opts := startOptions{
		root:        stackRoot,
		clients:     emails,
		randomPorts: true,
		reset:       true,
	}

	// Resolve absolute path
	var err error
	opts.root, err = filepath.Abs(opts.root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}

	t.Logf("Starting devstack at %s", opts.root)

	// Create root directory
	if err := os.MkdirAll(opts.root, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}

	relayRoot := filepath.Join(opts.root, relayDir)
	if err := os.MkdirAll(relayRoot, 0o755); err != nil {
		t.Fatalf("create relay dir: %v", err)
	}

	binDir := filepath.Join(relayRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	// Build binaries
	serverBin := addExe(filepath.Join(binDir, "server"))
	clientBin := addExe(filepath.Join(binDir, "syftbox"))

	// Find repository root (go up two levels from cmd/devstack)
	repoRoot, err := filepath.Abs(filepath.Join(".", "..", ".."))
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	t.Logf("Building server binary...")
	serverPkg := filepath.Join(repoRoot, "cmd", "server")
	if err := buildBinary(serverBin, serverPkg, serverBuildTags); err != nil {
		t.Fatalf("build server: %v", err)
	}

	t.Logf("Building client binary...")
	clientPkg := filepath.Join(repoRoot, "cmd", "client")
	if err := buildBinary(clientBin, clientPkg, clientBuildTags); err != nil {
		t.Fatalf("build client: %v", err)
	}

	// Allocate ports
	serverPort, err := getFreePort()
	if err != nil {
		t.Fatalf("allocate server port: %v", err)
	}

	minioAPIPort, err := getFreePort()
	if err != nil {
		t.Fatalf("allocate minio api port: %v", err)
	}

	minioConsolePort, err := getFreePort()
	if err != nil {
		t.Fatalf("allocate minio console port: %v", err)
	}

	// Start MinIO
	t.Logf("Starting MinIO on port %d...", minioAPIPort)
	minioMode := "local"
	minioBin, err := ensureMinioBinary(binDir)
	if err != nil {
		t.Fatalf("minio binary unavailable: %v", err)
	}

	mState, err := startMinio(minioMode, minioBin, relayRoot, minioAPIPort, minioConsolePort, false)
	if err != nil {
		t.Fatalf("start minio: %v", err)
	}
	defer stopMinio(mState)

	// Setup bucket
	t.Logf("Setting up MinIO bucket...")
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	if err := setupBucket(mState.APIPort); err != nil {
		t.Fatalf("minio bootstrap: %v", err)
	}

	// Start server
	t.Logf("Starting server on port %d...", serverPort)
	sState, err := startServer(serverBin, relayRoot, serverPort, mState.APIPort)
	if err != nil {
		stopMinio(mState)
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = killProcess(sState.PID)
	}()

	// Start clients
	t.Logf("Starting %d clients...", len(emails))
	var clients []clientState
	clientPortStart := 7938
	for i, email := range emails {
		port, err := getFreePort()
		if err != nil {
			t.Fatalf("allocate client port for %s: %v", email, err)
		}
		if !opts.randomPorts {
			port = clientPortStart + i
		}

		cState, err := startClient(clientBin, opts.root, email, serverURL, port, opts.extraEnv)
		if err != nil {
			t.Fatalf("start client %s: %v", email, err)
		}
		clients = append(clients, cState)
		defer func(pid int) {
			_ = killProcess(pid)
		}(cState.PID)
	}

	// Write state
	statePath := filepath.Join(relayRoot, stateFileName)
	state := stackState{
		Root:    opts.root,
		Server:  sState,
		Minio:   mState,
		Clients: clients,
		Created: time.Now().UTC(),
	}
	if err := writeState(statePath, &state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	t.Logf("Devstack started successfully")
	t.Logf("  Server: %s (pid %d)", serverURL, sState.PID)
	t.Logf("  MinIO:  http://127.0.0.1:%d", mState.APIPort)
	for _, c := range clients {
		t.Logf("  Client: %s (pid %d)", c.Email, c.PID)
	}

	// Run sync check
	t.Logf("Running sync check...")
	if err := runSyncCheck(opts.root, emails); err != nil {
		t.Fatalf("sync check failed: %v", err)
	}

	t.Logf("✅ Sync check passed - files replicated successfully")

	// Verify the probe file exists
	src := emails[0]
	for _, email := range emails[1:] {
		// Check that bob has alice's public files
		targetDir := filepath.Join(opts.root, email, "datasites", src, "public")
		entries, err := os.ReadDir(targetDir)
		if err != nil {
			t.Fatalf("read target dir %s: %v", targetDir, err)
		}
		t.Logf("Client %s synced %d files from %s", email, len(entries), src)
		if len(entries) < 1 {
			t.Fatalf("expected at least 1 file in %s, got %d", targetDir, len(entries))
		}
	}

	// Cleanup
	t.Logf("Stopping devstack...")
	if err := stopStack(opts.root); err != nil {
		t.Fatalf("stop stack: %v", err)
	}

	t.Logf("✅ Devstack integration test completed successfully")
}
