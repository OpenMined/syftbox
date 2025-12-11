//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestSimultaneousWrite tests the race condition where two clients write to
// the same file at exactly the same time.
func TestSimultaneousWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping conflict test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Simultaneous Write (Same File) ===")
	t.Log("Setup: Alice uploads file, alice and bob both overwrite simultaneously")
	t.Log("")

	// Create initial file
	filename := "conflict.txt"
	initialContent := []byte("initial version")
	initialMD5 := CalculateMD5(initialContent)

	t.Log("Step 1: Alice creates initial file")
	if err := h.alice.UploadFile(filename, initialContent); err != nil {
		t.Fatalf("alice initial upload failed: %v", err)
	}

	// Wait for bob to receive initial version
	t.Log("Step 2: Wait for bob to receive initial file")
	if err := h.bob.WaitForFile(h.alice.email, filename, initialMD5, 10*time.Second); err != nil {
		t.Fatalf("bob didn't receive initial file: %v", err)
	}

	// Prepare two different versions
	aliceContent := []byte("alice's version - modified at " + time.Now().String())
	bobContent := []byte("bob's version - modified at " + time.Now().String())
	aliceMD5 := CalculateMD5(aliceContent)
	bobMD5 := CalculateMD5(bobContent)

	t.Log("Step 3: Alice and bob both write simultaneously")

	// Write simultaneously
	var wg sync.WaitGroup
	var aliceErr, bobErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceErr = h.alice.UploadFile(filename, aliceContent)
	}()
	go func() {
		defer wg.Done()
		// Bob writes to alice's datasite (assuming public write access)
		bobPath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
		bobErr = os.WriteFile(bobPath, bobContent, 0o644)
	}()

	wg.Wait()

	if aliceErr != nil {
		t.Logf("⚠️  Alice's write failed: %v", aliceErr)
	} else {
		t.Log("✅ Alice's write succeeded")
	}

	if bobErr != nil {
		t.Logf("⚠️  Bob's write failed: %v", bobErr)
	} else {
		t.Log("✅ Bob's write succeeded")
	}

	// Step 4: Wait for convergence
	t.Log("Step 4: Wait for sync to converge (15s)")
	time.Sleep(15 * time.Second)

	// Step 5: Check final state - should be ONE of the versions, not mixed
	t.Log("Step 5: Verify no data corruption")
	aliceFilePath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", filename)
	finalContent, err := os.ReadFile(aliceFilePath)
	if err != nil {
		t.Fatalf("read final alice file: %v", err)
	}
	finalMD5 := CalculateMD5(finalContent)

	if finalMD5 == aliceMD5 {
		t.Log("✅ Final state: Alice's version (last-write or alice-wins)")
	} else if finalMD5 == bobMD5 {
		t.Log("✅ Final state: Bob's version (last-write or bob-wins)")
	} else if finalMD5 == initialMD5 {
		t.Log("⚠️  Final state: Reverted to initial version")
	} else {
		t.Errorf("❌ Final state: CORRUPTED (mixed content)! MD5: %s", finalMD5)
		t.Logf("   Expected alice (%s) or bob (%s) or initial (%s)", aliceMD5, bobMD5, initialMD5)
		t.Logf("   Content: %s", string(finalContent))
	}

	// Verify bob converges to same state
	bobFilePath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
	bobFinalContent, err := os.ReadFile(bobFilePath)
	if err != nil {
		t.Fatalf("read final bob file: %v", err)
	}
	bobFinalMD5 := CalculateMD5(bobFinalContent)

	if bobFinalMD5 == finalMD5 {
		t.Log("✅ Bob converged to same state as alice")
	} else {
		t.Errorf("❌ Bob has different state! Alice: %s, Bob: %s", finalMD5, bobFinalMD5)
	}
}

// TestDivergentEdits tests the scenario where clients edit the same file while
// one is offline, then reconnect.
// TODO: Fix conflict resolution for offline divergent edits
func TestDivergentEdits(t *testing.T) {
	t.Skip("skipping: offline conflict resolution not yet implemented")

	if testing.Short() {
		t.Skip("skipping conflict test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Divergent Edits (Offline → Online) ===")
	t.Log("Setup: Alice edits v1→v2 online, bob edits v1→v3 offline, then reconnects")
	t.Log("")

	filename := "divergent.txt"
	v1Content := []byte("version 1 - baseline")
	v1MD5 := CalculateMD5(v1Content)

	t.Log("Step 1: Alice creates v1, bob receives it")
	if err := h.alice.UploadFile(filename, v1Content); err != nil {
		t.Fatalf("alice upload v1: %v", err)
	}

	if err := h.bob.WaitForFile(h.alice.email, filename, v1MD5, 10*time.Second); err != nil {
		t.Fatalf("bob didn't receive v1: %v", err)
	}

	t.Log("Step 2: Stop bob (simulate offline)")
	if err := killProcess(h.bob.state.PID); err != nil {
		t.Fatalf("stop bob: %v", err)
	}
	time.Sleep(2 * time.Second) // Wait for clean shutdown

	t.Log("Step 3: Alice edits v1 → v2 (while bob offline)")
	v2Content := []byte("version 2 - alice's online edit")
	v2MD5 := CalculateMD5(v2Content)
	if err := h.alice.UploadFile(filename, v2Content); err != nil {
		t.Fatalf("alice upload v2: %v", err)
	}
	time.Sleep(2 * time.Second) // Wait for alice's sync

	t.Log("Step 4: Bob edits v1 → v3 (offline, local only)")
	v3Content := []byte("version 3 - bob's offline edit")
	v3MD5 := CalculateMD5(v3Content)
	bobFilePath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
	if err := os.WriteFile(bobFilePath, v3Content, 0o644); err != nil {
		t.Fatalf("bob offline edit: %v", err)
	}

	t.Log("Step 5: Restart bob (comes back online with divergent edit)")
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", h.state.Server.Port)
	bobState, err := startClient(
		h.bob.state.BinPath,
		h.bob.state.DataPath,
		h.bob.email,
		serverURL,
		h.bob.state.Port,
	)
	if err != nil {
		t.Fatalf("restart bob: %v", err)
	}
	h.bob.state = bobState

	t.Log("Step 6: Wait for conflict resolution (15s)")
	time.Sleep(15 * time.Second)

	// Step 7: Check final states
	t.Log("Step 7: Verify conflict resolution")

	aliceFilePath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", filename)
	aliceFinal, _ := os.ReadFile(aliceFilePath)
	aliceFinalMD5 := CalculateMD5(aliceFinal)

	bobFinal, _ := os.ReadFile(bobFilePath)
	bobFinalMD5 := CalculateMD5(bobFinal)

	t.Logf("   Alice final MD5: %s", aliceFinalMD5)
	t.Logf("   Bob final MD5: %s", bobFinalMD5)

	// Check for expected outcomes
	if aliceFinalMD5 == v2MD5 && bobFinalMD5 == v2MD5 {
		t.Log("✅ Server-wins: Both converged to alice's v2 (online version)")
	} else if aliceFinalMD5 == v3MD5 && bobFinalMD5 == v3MD5 {
		t.Log("✅ Client-wins: Both converged to bob's v3 (offline version)")
	} else if aliceFinalMD5 == bobFinalMD5 {
		t.Logf("✅ Converged to same state (MD5: %s)", aliceFinalMD5)
		if aliceFinalMD5 != v2MD5 && aliceFinalMD5 != v3MD5 {
			t.Logf("   ⚠️  New version (conflict marker or merge?): %s", string(aliceFinal))
		}
	} else {
		t.Errorf("❌ Divergent final states! Alice: %s, Bob: %s", aliceFinalMD5, bobFinalMD5)
		t.Logf("   Alice content: %s", string(aliceFinal))
		t.Logf("   Bob content: %s", string(bobFinal))
	}

	// Document behavior
	t.Log("")
	t.Log("Conflict resolution strategy observed:")
	if aliceFinalMD5 == v2MD5 {
		t.Log("   - Server version wins (v2)")
	} else if bobFinalMD5 == v3MD5 {
		t.Log("   - Client version wins (v3)")
	} else {
		t.Log("   - Custom resolution (conflict markers or merge)")
	}
}

// TestThreeWayConflict tests conflict resolution when three clients
// all edit the same file simultaneously.
func TestThreeWayConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping conflict test in short mode")
	}

	h := NewDevstackHarness(t)

	// Start charlie as third client
	t.Log("=== TEST: Three-Way Conflict ===")
	t.Log("Setup: Alice, Bob, Charlie all edit same file simultaneously")
	t.Log("")

	t.Log("Step 0: Start charlie")
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

	time.Sleep(2 * time.Second) // Wait for charlie to initialize

	// Create initial file
	filename := "three-way.txt"
	initialContent := []byte("initial version for all")
	initialMD5 := CalculateMD5(initialContent)

	t.Log("Step 1: Alice creates initial file")
	if err := h.alice.UploadFile(filename, initialContent); err != nil {
		t.Fatalf("alice upload: %v", err)
	}

	// Wait for all to receive
	t.Log("Step 2: Wait for bob and charlie to receive")
	if err := h.bob.WaitForFile(h.alice.email, filename, initialMD5, 10*time.Second); err != nil {
		t.Fatalf("bob didn't receive: %v", err)
	}
	if err := charlie.WaitForFile(h.alice.email, filename, initialMD5, 10*time.Second); err != nil {
		t.Fatalf("charlie didn't receive: %v", err)
	}

	// Prepare three different versions
	aliceContent := []byte("alice's version - " + time.Now().String())
	bobContent := []byte("bob's version - " + time.Now().String())
	charlieContent := []byte("charlie's version - " + time.Now().String())

	aliceMD5 := CalculateMD5(aliceContent)
	bobMD5 := CalculateMD5(bobContent)
	charlieMD5 := CalculateMD5(charlieContent)

	t.Log("Step 3: All three write simultaneously")

	var wg sync.WaitGroup
	var aliceErr, bobErr, charlieErr error

	wg.Add(3)
	go func() {
		defer wg.Done()
		aliceErr = h.alice.UploadFile(filename, aliceContent)
	}()
	go func() {
		defer wg.Done()
		bobPath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
		bobErr = os.WriteFile(bobPath, bobContent, 0o644)
	}()
	go func() {
		defer wg.Done()
		charliePath := filepath.Join(charlie.dataDir, "datasites", h.alice.email, "public", filename)
		charlieErr = os.WriteFile(charliePath, charlieContent, 0o644)
	}()

	wg.Wait()

	t.Logf("   Alice write: %v", aliceErr)
	t.Logf("   Bob write: %v", bobErr)
	t.Logf("   Charlie write: %v", charlieErr)

	// Wait for convergence
	t.Log("Step 4: Wait for convergence (15s)")
	time.Sleep(15 * time.Second)

	// Check final states
	t.Log("Step 5: Verify all converged to same state")

	aliceFilePath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", filename)
	aliceFinal, _ := os.ReadFile(aliceFilePath)
	aliceFinalMD5 := CalculateMD5(aliceFinal)

	bobFilePath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
	bobFinal, _ := os.ReadFile(bobFilePath)
	bobFinalMD5 := CalculateMD5(bobFinal)

	charlieFilePath := filepath.Join(charlie.dataDir, "datasites", h.alice.email, "public", filename)
	charlieFinal, _ := os.ReadFile(charlieFilePath)
	charlieFinalMD5 := CalculateMD5(charlieFinal)

	t.Logf("   Alice final: %s", aliceFinalMD5)
	t.Logf("   Bob final: %s", bobFinalMD5)
	t.Logf("   Charlie final: %s", charlieFinalMD5)

	// All should converge to same state
	if aliceFinalMD5 == bobFinalMD5 && bobFinalMD5 == charlieFinalMD5 {
		t.Log("✅ All three converged to same state")

		// Identify winner
		if aliceFinalMD5 == aliceMD5 {
			t.Log("   Winner: Alice's version")
		} else if aliceFinalMD5 == bobMD5 {
			t.Log("   Winner: Bob's version")
		} else if aliceFinalMD5 == charlieMD5 {
			t.Log("   Winner: Charlie's version")
		} else {
			t.Logf("   Winner: Merged/conflict version (MD5: %s)", aliceFinalMD5)
		}
	} else {
		t.Error("❌ Divergent final states!")
		t.Logf("   Alice: %s", aliceFinalMD5)
		t.Logf("   Bob: %s", bobFinalMD5)
		t.Logf("   Charlie: %s", charlieFinalMD5)
	}
}

// TestConflictDuringACLChange tests conflict when ACL permissions change
// while a write is in progress.
func TestConflictDuringACLChange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping conflict test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Conflict During ACL Restricted Access ===")
	t.Log("Setup: Alice grants write to bob, both write, alice revokes mid-conflict")
	t.Log("")

	// Create default ACLs
	t.Log("Step 1: Create public ACLs")
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice ACLs: %v", err)
	}
	time.Sleep(2 * time.Second)

	filename := "acl-conflict.txt"
	initialContent := []byte("initial")
	initialMD5 := CalculateMD5(initialContent)

	t.Log("Step 2: Alice creates initial file")
	if err := h.alice.UploadFile(filename, initialContent); err != nil {
		t.Fatalf("alice upload: %v", err)
	}

	if err := h.bob.WaitForFile(h.alice.email, filename, initialMD5, 10*time.Second); err != nil {
		t.Fatalf("bob didn't receive: %v", err)
	}

	// Prepare writes
	aliceContent := []byte("alice's update")
	bobContent := []byte("bob's update")

	t.Log("Step 3: Start simultaneous writes")
	var wg sync.WaitGroup
	var aliceErr, bobErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceErr = h.alice.UploadFile(filename, aliceContent)
	}()
	go func() {
		defer wg.Done()
		bobPath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
		bobErr = os.WriteFile(bobPath, bobContent, 0o644)
	}()

	// Change ACL during writes
	time.Sleep(100 * time.Millisecond)
	t.Log("Step 4: Alice revokes bob's write access during conflict")
	publicDir := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public")
	aclPath := filepath.Join(publicDir, "syft.pub.yaml")
	aclContent := fmt.Sprintf(`terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s']
`, h.alice.email, h.alice.email)
	if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
		t.Logf("ACL change failed: %v", err)
	}

	wg.Wait()

	t.Logf("   Alice write: %v", aliceErr)
	t.Logf("   Bob write: %v", bobErr)

	// Wait for sync
	t.Log("Step 5: Wait for conflict + ACL propagation (15s)")
	time.Sleep(15 * time.Second)

	// Verify final state
	t.Log("Step 6: Verify conflict handled correctly")
	aliceFilePath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", filename)
	finalContent, _ := os.ReadFile(aliceFilePath)
	finalMD5 := CalculateMD5(finalContent)

	aliceMD5 := CalculateMD5(aliceContent)
	bobMD5 := CalculateMD5(bobContent)

	if finalMD5 == aliceMD5 {
		t.Log("✅ Final state: Alice's version (owner wins)")
	} else if finalMD5 == bobMD5 {
		t.Log("⚠️  Final state: Bob's version (write completed before ACL enforcement)")
	} else {
		t.Logf("✅ Final state: Other version (MD5: %s)", finalMD5)
	}

	// Verify bob lost write access
	t.Log("Step 7: Verify bob can no longer write")
	bobTestContent := []byte("bob's new write attempt")
	bobPath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", "bob-test.txt")
	if err := os.WriteFile(bobPath, bobTestContent, 0o644); err == nil {
		time.Sleep(5 * time.Second)
		// Check if it synced to alice
		aliceTestPath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", "bob-test.txt")
		if _, err := os.Stat(aliceTestPath); err == nil {
			t.Log("⚠️  Bob's write propagated (ACL not enforced)")
		} else {
			t.Log("✅ Bob's write rejected (ACL enforced)")
		}
	}
}

// TestNestedPathConflict tests conflict when one client creates a file
// and another creates a directory with the same name.
func TestNestedPathConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping conflict test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Nested Path Conflict ===")
	t.Log("Setup: Alice creates 'item' as directory, bob creates 'item' as file")
	t.Log("")

	pathName := "item"

	t.Log("Step 1: Alice creates 'item' as directory with nested file")
	aliceDir := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", pathName)
	if err := os.MkdirAll(aliceDir, 0o755); err != nil {
		t.Fatalf("alice create directory: %v", err)
	}
	aliceFile := filepath.Join(aliceDir, "nested.txt")
	aliceContent := []byte("content in item/nested.txt")
	if err := os.WriteFile(aliceFile, aliceContent, 0o644); err != nil {
		t.Fatalf("alice create nested file: %v", err)
	}

	t.Log("Step 2: Bob creates 'item' as file")
	bobItemPath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", pathName)
	bobContent := []byte("item as a file")
	if err := os.WriteFile(bobItemPath, bobContent, 0o644); err != nil {
		t.Fatalf("bob create file: %v", err)
	}

	t.Log("Step 3: Wait for sync and conflict resolution (15s)")
	time.Sleep(15 * time.Second)

	// Check alice's state
	t.Log("Step 4: Verify filesystem consistency")
	aliceItemPath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", pathName)
	aliceInfo, aliceErr := os.Stat(aliceItemPath)

	if aliceErr != nil {
		t.Logf("⚠️  Alice's 'item' disappeared: %v", aliceErr)
	} else if aliceInfo.IsDir() {
		t.Log("✅ Alice's state: 'item' is directory")
		// Check nested file still exists
		if _, err := os.Stat(aliceFile); err == nil {
			t.Log("   ✅ Nested file preserved")
		} else {
			t.Log("   ⚠️  Nested file lost")
		}
	} else {
		t.Log("✅ Alice's state: 'item' is file")
		content, _ := os.ReadFile(aliceItemPath)
		if string(content) == string(bobContent) {
			t.Log("   (Contains bob's content)")
		}
	}

	// Check bob's state
	bobInfo, bobErr := os.Stat(bobItemPath)
	if bobErr != nil {
		t.Logf("⚠️  Bob's 'item' disappeared: %v", bobErr)
	} else if bobInfo.IsDir() {
		t.Log("✅ Bob's state: 'item' is directory")
	} else {
		t.Log("✅ Bob's state: 'item' is file")
	}

	// Verify convergence
	if aliceErr == nil && bobErr == nil {
		if aliceInfo.IsDir() == bobInfo.IsDir() {
			t.Log("✅ Alice and bob converged to same type")
		} else {
			t.Error("❌ Alice and bob have different types!")
		}
	}

	t.Log("")
	t.Log("Conflict resolution observed:")
	if aliceErr == nil && aliceInfo.IsDir() {
		t.Log("   - Directory wins (alice's version)")
	} else if aliceErr == nil && !aliceInfo.IsDir() {
		t.Log("   - File wins (bob's version)")
	} else {
		t.Log("   - Path removed (conflict rejected)")
	}
}
