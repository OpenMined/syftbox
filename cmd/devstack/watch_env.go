//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// maybeWriteWatchEnv writes SYFTBOX_CLIENT_URL/TOKEN to a file if SBDEV_WATCH_ENV is set.
// This lets just recipes start a test and a watcher without manual copy/paste.
func maybeWriteWatchEnv(t *testing.T, clientURL, authToken string) {
	t.Helper()
	target := os.Getenv("SBDEV_WATCH_ENV")
	if target == "" {
		// If running under just/devstack with PERF_TEST_SANDBOX, always emit a default watch file.
		if sandbox := os.Getenv("PERF_TEST_SANDBOX"); sandbox != "" {
			target = filepath.Join(sandbox, "watch.env")
		}
	}
	if target == "" || clientURL == "" || authToken == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Logf("watch env: mkdir failed: %v", err)
		return
	}
	payload := fmt.Sprintf("SYFTBOX_CLIENT_URL=%s\nSYFTBOX_CLIENT_TOKEN=%s\n", clientURL, authToken)
	if err := os.WriteFile(target, []byte(payload), 0o644); err != nil {
		t.Logf("watch env: write failed: %v", err)
		return
	}
	t.Logf("watch env written to %s", target)
}
