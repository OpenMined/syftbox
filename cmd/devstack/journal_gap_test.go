//go:build integration
// +build integration

package main

import (
	"bufio"
	"crypto/md5"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestJournalGapSpuriousConflict demonstrates the race condition where:
// 1. File is downloaded to Bob (local exists, remote exists)
// 2. Journal write hasn't completed yet (journal entry missing)
// 3. Alice modifies the file (remote changes)
// 4. Bob's sync sees: local != remote, no journal → CONFLICT
//
// This is a spurious conflict because Bob just downloaded the file.
// The bug is in hasModified() returning true when journal is nil.
func TestJournalGapSpuriousConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Keep the devstack sandbox inside the repo so go test doesn't need to write outside
	// the workspace sandbox.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Setenv("PERF_TEST_SANDBOX", filepath.Join(cwd, ".test-sandbox", "journal-gap-spurious"))

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Journal Gap Spurious Conflict ===")
	t.Log("This test reproduces the race condition where missing journal")
	t.Log("entries cause spurious conflicts when remote changes.")
	t.Log("")

	// Setup ACLs for both clients
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob ACLs: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Use public folder for simpler ACL handling
	filename := "journal-race-test.txt"
	alicePath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", filename)
	bobMirrorPath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
	journalPath := filepath.Join(h.bob.dataDir, ".data", "sync.db")

	// Step 1: Alice creates a file
	t.Log("Step 1: Alice creates file v1")
	v1Content := []byte("version 1 - original content")
	v1MD5 := fmt.Sprintf("%x", md5.Sum(v1Content))
	if err := os.WriteFile(alicePath, v1Content, 0o644); err != nil {
		t.Fatalf("alice write v1: %v", err)
	}
	t.Logf("   v1 MD5: %s", v1MD5)

	// Step 2: Wait for Bob to receive file AND have journal entry
	t.Log("Step 2: Wait for Bob to receive file and journal entry")
	if err := waitForJournalEntry(journalPath, filename, v1MD5, 15*time.Second); err != nil {
		t.Fatalf("bob journal entry not found: %v", err)
	}

	// Verify file exists on Bob
	bobContent, err := os.ReadFile(bobMirrorPath)
	if err != nil {
		t.Fatalf("bob mirror file not found: %v", err)
	}
	bobMD5 := fmt.Sprintf("%x", md5.Sum(bobContent))
	t.Logf("   Bob has file (MD5: %s) and journal entry", bobMD5)

	// Step 3: STOP Bob to prevent any sync while we set up the race condition
	t.Log("Step 3: Stop Bob (prevent sync while setting up race)")
	if err := killProcess(h.bob.state.PID); err != nil {
		t.Fatalf("stop bob: %v", err)
	}
	t.Logf("   Bob stopped (was pid %d)", h.bob.state.PID)
	time.Sleep(500 * time.Millisecond)

	// Step 4: Delete Bob's journal entry (while Bob is stopped)
	t.Log("Step 4: Delete Bob's journal entry")

	// Debug: Show all journal entries before deletion
	t.Log("   DEBUG: Journal entries before deletion:")
	if entries, err := listJournalEntries(journalPath); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Path, "journal-race-test") || strings.Contains(e.Path, "public") {
				t.Logf("      %s -> %s", e.Path, e.ETag)
			}
		}
	}

	if err := deleteJournalEntrySingle(journalPath, filename); err != nil {
		t.Fatalf("delete journal entry: %v", err)
	}

	// Debug: Show all journal entries after deletion
	t.Log("   DEBUG: Journal entries after deletion:")
	if entries, err := listJournalEntries(journalPath); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Path, "journal-race-test") || strings.Contains(e.Path, "public") {
				t.Logf("      %s -> %s", e.Path, e.ETag)
			}
		}
	}
	t.Log("   Journal entry deleted")

	// Step 5: Alice modifies file and uploads to server
	t.Log("Step 5: Alice modifies file to v2 and uploads")
	v2Content := []byte("version 2 - MODIFIED by Alice!")
	v2MD5 := fmt.Sprintf("%x", md5.Sum(v2Content))
	if err := os.WriteFile(alicePath, v2Content, 0o644); err != nil {
		t.Fatalf("alice write v2: %v", err)
	}
	t.Logf("   v2 MD5: %s", v2MD5)

	// Wait for Alice's change to reach server - verify it's actually uploaded
	t.Log("   Waiting for Alice's sync to upload v2 to server...")
	if err := triggerClientSync(t, h.alice); err != nil {
		t.Logf("   Warning: could not trigger Alice sync: %v", err)
	}
	// Poll until server has v2 (up to 10 seconds)
	serverHasV2 := false
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if err := triggerClientSync(t, h.alice); err == nil {
			// Check Alice's journal to verify upload completed
			aliceJournalPath := filepath.Join(h.alice.dataDir, ".data", "sync.db")
			if entry, err := getJournalEntry(aliceJournalPath, filename); err == nil {
				if entry.ETag == v2MD5 || strings.HasPrefix(entry.ETag, v2MD5[:10]) {
					t.Logf("   Server has v2 (verified via Alice journal after %d polls)", i+1)
					serverHasV2 = true
					break
				}
			}
		}
	}
	if !serverHasV2 {
		t.Log("   Warning: Could not verify v2 on server, continuing anyway")
	}

	t.Log("")
	t.Log("   Race condition state NOW (Bob is stopped):")
	t.Log("   - Bob local file: v1")
	t.Log("   - Server:         v2 (Alice uploaded)")
	t.Log("   - Bob journal:    MISSING (deleted)")
	t.Log("")
	t.Log("   When Bob restarts, expected behavior:")
	t.Log("   healJournalGaps: local(v1) != remote(v2) → CANNOT heal")
	t.Log("   hasModified(local=v1, journal=nil) → true")
	t.Log("   hasModified(journal=nil, remote=v2) → true")
	t.Log("   Both true → SPURIOUS CONFLICT!")

	// Step 6: Restart Bob
	t.Log("")
	t.Log("Step 6: Restart Bob")
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", h.state.Server.Port)
	rootDir := filepath.Dir(h.bob.state.DataPath)
	bobState, err := startClient(
		h.bob.state.BinPath,
		rootDir,
		h.bob.email,
		serverURL,
		h.bob.state.Port,
	)
	if err != nil {
		t.Fatalf("restart bob: %v", err)
	}
	h.bob.state = bobState
	t.Logf("   Bob restarted (new pid %d)", bobState.PID)

	// Step 7: Wait for Bob's first sync cycle
	t.Log("Step 7: Wait for Bob's sync cycle (5 seconds)")
	time.Sleep(5 * time.Second)

	// Debug: Show Bob's log file for sync activity
	t.Log("")
	t.Log("   DEBUG: Bob's sync log (last 50 lines):")
	if logContent, err := os.ReadFile(h.bob.state.LogPath); err == nil {
		lines := strings.Split(string(logContent), "\n")
		start := len(lines) - 50
		if start < 0 {
			start = 0
		}
		for _, line := range lines[start:] {
			if strings.Contains(line, "CONFLICT") ||
				strings.Contains(line, "conflict") ||
				strings.Contains(line, "healed") ||
				strings.Contains(line, "full sync") ||
				strings.Contains(line, "downloads") {
				t.Logf("   %s", line)
			}
		}
	}

	// Debug: Check journal state after Bob's sync
	t.Log("")
	t.Log("   DEBUG: Journal state after sync:")
	if entry, err := getJournalEntry(journalPath, filename); err == nil {
		t.Logf("   - Journal entry exists: etag=%s", entry.ETag)
	} else {
		t.Logf("   - Journal entry: MISSING (%v)", err)
	}

	// Debug: Check Bob's file state
	if content, err := os.ReadFile(bobMirrorPath); err == nil {
		md5sum := fmt.Sprintf("%x", md5.Sum(content))
		t.Logf("   - Bob's file: exists, MD5=%s", md5sum)
		if md5sum == v1MD5 {
			t.Log("   - Content: v1 (original)")
		} else if md5sum == v2MD5 {
			t.Log("   - Content: v2 (modified)")
		}
	} else {
		t.Logf("   - Bob's file: %v", err)
	}

	// Step 8: Check for spurious conflicts on Bob
	t.Log("")
	t.Log("Step 8: Check for spurious conflicts on Bob")
	conflicts := findFilesRecursive(h.bob.dataDir, ".conflict")

	// Filter to only conflicts for our test file
	var relevantConflicts []string
	for _, c := range conflicts {
		if strings.Contains(c, "journal-race-test") {
			relevantConflicts = append(relevantConflicts, c)
		}
	}

	if len(relevantConflicts) > 0 {
		t.Logf("Found %d unexpected conflict(s):", len(relevantConflicts))
		for _, c := range relevantConflicts {
			t.Logf("   - %s", filepath.Base(c))
		}
		t.Fatalf("unexpected conflict marker(s) created for mirrored path while journal entry was missing")
	} else {
		// Check Bob's current state
		bobFinal, err := os.ReadFile(bobMirrorPath)
		if err != nil {
			t.Logf("Warning: could not read bob's file: %v", err)
		} else {
			bobFinalMD5 := fmt.Sprintf("%x", md5.Sum(bobFinal))
			if bobFinalMD5 == v2MD5 {
				t.Log("   Bob received v2 without conflict")
				t.Log("   Either the bug is fixed, or server-wins behavior applied")
			} else if bobFinalMD5 == v1MD5 {
				t.Log("   Bob still has v1 (sync may not have completed)")
			} else {
				t.Logf("   Bob has unexpected content (MD5: %s)", bobFinalMD5)
			}
		}
		t.Log("")
		t.Log("No spurious conflicts found")
	}

	t.Log("")
	t.Log("=== Test Complete ===")
}

// deleteJournalEntrySingle removes a specific entry from the sync journal
func deleteJournalEntrySingle(journalPath, pathPattern string) error {
	db, err := sql.Open("sqlite3", journalPath)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer db.Close()

	result, err := db.Exec("DELETE FROM sync_journal WHERE path LIKE ?", "%"+pathPattern+"%")
	if err != nil {
		return fmt.Errorf("delete from journal: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("no matching journal entry found for pattern: %s", pathPattern)
	}
	return nil
}

// countJournalEntriesSingle counts entries matching a pattern
func countJournalEntriesSingle(journalPath, pathPattern string) (int, error) {
	db, err := sql.Open("sqlite3", journalPath)
	if err != nil {
		return 0, fmt.Errorf("open journal: %w", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sync_journal WHERE path LIKE ?", "%"+pathPattern+"%").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count journal entries: %w", err)
	}
	return count, nil
}

// listJournalEntries returns all journal entries
func listJournalEntries(journalPath string) ([]journalEntry, error) {
	db, err := sql.Open("sqlite3", journalPath)
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT path, etag FROM sync_journal")
	if err != nil {
		return nil, fmt.Errorf("query journal: %w", err)
	}
	defer rows.Close()

	var entries []journalEntry
	for rows.Next() {
		var e journalEntry
		if err := rows.Scan(&e.Path, &e.ETag); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func TestJournalGapHealing(t *testing.T) {
	// Use a persistent suite so binaries are built only once across subtests.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Setenv("PERF_TEST_SANDBOX", filepath.Join(cwd, ".test-sandbox", "journal-gap-suite"))

	t.Run("healing_enabled", func(t *testing.T) {
		t.Setenv("SYFTBOX_SYNC_HEAL_JOURNAL_GAPS", "1")
		runJournalGapScenario(t, false)
	})

	t.Run("healing_disabled_reproduces_conflicts", func(t *testing.T) {
		t.Setenv("SYFTBOX_SYNC_HEAL_JOURNAL_GAPS", "0")
		runJournalGapScenario(t, true)
	})
}

func runJournalGapScenario(t *testing.T, expectConflicts bool) {
	t.Helper()

	h := NewDevstackHarness(t)
	t.Cleanup(h.Cleanup)

	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	appName := "perftest"
	endpoint := "journal-gap"
	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}

	// Let the stack settle.
	time.Sleep(1 * time.Second)

	const (
		numFiles = 5
		fileSize = 1024
	)

	filenames := make([]string, 0, numFiles)
	md5Hashes := make([]string, 0, numFiles)
	journalPaths := make([]string, 0, numFiles)
	for i := 0; i < numFiles; i++ {
		content := GenerateRandomFile(fileSize)
		filename := fmt.Sprintf("jg-file-%d.request", i)
		filenames = append(filenames, filename)
		md5Hashes = append(md5Hashes, CalculateMD5(content))
		journalPaths = append(journalPaths, fmt.Sprintf("%s/app_data/%s/rpc/%s/%s", h.alice.email, appName, endpoint, filename))

		if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
			t.Fatalf("upload %s: %v", filename, err)
		}
	}

	for i, filename := range filenames {
		if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hashes[i], 10*time.Second); err != nil {
			t.Fatalf("wait for %s: %v", filename, err)
		}
	}

	syncDBPath := filepath.Join(h.bob.dataDir, ".data", "sync.db")
	if err := deleteJournalEntries(t, syncDBPath, journalPaths); err != nil {
		t.Fatalf("delete journal entries: %v", err)
	}

	if err := triggerClientSync(t, h.bob); err != nil {
		t.Fatalf("trigger bob sync: %v", err)
	}
	time.Sleep(750 * time.Millisecond)

	conflictPaths := make([]string, 0, len(filenames))
	for _, filename := range filenames {
		base := strings.TrimSuffix(filename, filepath.Ext(filename))
		ext := filepath.Ext(filename)
		conflictName := base + ".conflict" + ext
		conflictPaths = append(conflictPaths, filepath.Join(
			h.bob.dataDir,
			"datasites",
			h.alice.email,
			"app_data",
			appName,
			"rpc",
			endpoint,
			conflictName,
		))
	}

	found := make([]string, 0)
	for _, p := range conflictPaths {
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
	}

	if expectConflicts && len(found) == 0 {
		t.Fatalf("expected conflict markers after deleting journal entries, but found none")
	}
	if !expectConflicts && len(found) > 0 {
		t.Fatalf("unexpected conflict markers after deleting journal entries: %v", found)
	}
}

func deleteJournalEntries(t *testing.T, dbPath string, paths []string) error {
	t.Helper()
	if len(paths) == 0 {
		return nil
	}

	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	placeholders := strings.TrimRight(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, 0, len(paths))
	for _, p := range paths {
		args = append(args, p)
	}
	_, err = conn.Exec("DELETE FROM sync_journal WHERE path IN ("+placeholders+")", args...) //nolint:sqlclosecheck
	return err //nolint:wrapcheck
}

func triggerClientSync(t *testing.T, client *ClientHelper) error {
	t.Helper()

	token, err := readControlPlaneToken(client.state.LogPath)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/sync/now", client.state.Port)

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sync trigger status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func readControlPlaneToken(logPath string) (string, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var token string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "control plane start") {
			continue
		}
		idx := strings.Index(line, " token=")
		if idx == -1 {
			continue
		}
		token = strings.TrimSpace(line[idx+len(" token="):])
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("control plane token not found in %s", logPath)
	}
	return token, nil
}
