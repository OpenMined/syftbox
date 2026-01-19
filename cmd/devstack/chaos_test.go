//go:build integration
// +build integration

package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestChaosSync exercises random mixed operations across three clients to shake out race conditions.
func TestChaosSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	// Allow reproducible runs via CHAOS_SEED environment variable
	seed := time.Now().UnixNano()
	if seedStr := os.Getenv("CHAOS_SEED"); seedStr != "" {
		if parsedSeed, err := strconv.ParseInt(seedStr, 10, 64); err == nil {
			seed = parsedSeed
			t.Logf("Using CHAOS_SEED from environment: %d", seed)
		}
	}
	t.Logf("Chaos test seed: %d (set CHAOS_SEED=%d to reproduce)", seed, seed)
	rng := rand.New(rand.NewSource(seed))

	h := NewDevstackHarness(t)

	// Start a third client (charlie) manually using the existing binaries and server.
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

	charlieHelper := &ClientHelper{
		t:         t,
		email:     charlieState.Email,
		state:     charlieState,
		dataDir:   charlieState.DataPath,
		publicDir: filepath.Join(charlieState.DataPath, "datasites", charlieState.Email, "public"),
		metrics:   &ClientMetrics{},
	}

	// Wait for all clients to complete initial sync and start file watchers
	// This ensures forceBroadcastPublicACL will trigger watcher events
	t.Logf("Waiting for clients to initialize...")
	time.Sleep(2 * time.Second)

	// Ensure default ACLs for all participants.
	for _, c := range []*ClientHelper{h.alice, h.bob, charlieHelper} {
		if err := c.CreateDefaultACLs(); err != nil {
			t.Fatalf("create default ACLs for %s: %v", c.email, err)
		}
		// Rewrite the ACL to force a priority upload broadcast.
		if err := forceBroadcastPublicACL(c); err != nil {
			t.Fatalf("broadcast default ACL for %s: %v", c.email, err)
		}
	}

	// Wait for baseline public ACLs to be present on all peers before chaos begins.
	t.Logf("[DEBUG] Waiting for baseline ACL propagation...")
	for _, c := range []*ClientHelper{h.alice, h.bob, charlieHelper} {
		aclPath := filepath.Join(c.publicDir, "syft.pub.yaml")
		data, err := os.ReadFile(aclPath)
		if err != nil {
			t.Fatalf("read baseline acl for %s: %v", c.email, err)
		}
		t.Logf("[DEBUG] %s baseline ACL (md5=%s):\n%s", c.email, CalculateMD5(data), string(data))
		waitForACLPropagation(t, c, []*ClientHelper{h.alice, h.bob, charlieHelper}, string(data), 15*time.Second)
		t.Logf("[DEBUG] %s baseline ACL propagated successfully to all peers", c.email)
	}
	t.Logf("[DEBUG] All baseline ACLs propagated - starting chaos operations")

	clients := []*ClientHelper{h.alice, h.bob, charlieHelper}

	// Track expected files per client (updated as ACLs change)
	type fileInfo struct {
		owner string
		path  string
		md5   string
		isRPC bool
	}
	expectedFiles := make(map[string]map[string]fileInfo) // clientEmail -> filePath -> fileInfo
	for _, c := range clients {
		expectedFiles[c.email] = make(map[string]fileInfo)
	}

	// Track current ACL state per owner: which peers have read access
	aclState := make(map[string][]string) // ownerEmail -> []allowedPeerEmails
	// Initialize with public access for all
	for _, owner := range clients {
		var peers []string
		for _, peer := range clients {
			if peer.email != owner.email {
				peers = append(peers, peer.email)
			}
		}
		aclState[owner.email] = peers
	}

	// Helper functions
	contains := func(slice []string, item string) bool {
		for _, s := range slice {
			if s == item {
				return true
			}
		}
		return false
	}

	// Helper: add file to expected files for allowed peers based on current ACL
	addFileToExpected := func(owner, path, md5 string, isRPC bool) {
		fileKey := fmt.Sprintf("%s/%s", owner, path)
		// Always add to owner's expected files
		expectedFiles[owner][fileKey] = fileInfo{owner: owner, path: path, md5: md5, isRPC: isRPC}
		// Add to allowed peers
		for _, peerEmail := range aclState[owner] {
			expectedFiles[peerEmail][fileKey] = fileInfo{owner: owner, path: path, md5: md5, isRPC: isRPC}
		}
	}

	// Helper: update expected files when ACL changes for an owner
	updateACLState := func(owner string, newAllowedPeers []string) {
		oldAllowed := aclState[owner]
		aclState[owner] = newAllowedPeers

		// For each file owned by this owner, update which peers should have it
		for peerEmail, files := range expectedFiles {
			if peerEmail == owner {
				continue // owner always has their own files
			}

			wasAllowed := contains(oldAllowed, peerEmail)
			nowAllowed := contains(newAllowedPeers, peerEmail)

			if wasAllowed && !nowAllowed {
				// Peer lost access - remove all owner's files from this peer
				for fileKey, info := range files {
					if info.owner == owner {
						delete(expectedFiles[peerEmail], fileKey)
					}
				}
			} else if !wasAllowed && nowAllowed {
				// Peer gained access - add all owner's files to this peer
				// Find all files owned by this owner from any peer's expected list
				for _, otherFiles := range expectedFiles {
					for fileKey, info := range otherFiles {
						if info.owner == owner {
							expectedFiles[peerEmail][fileKey] = info
						}
					}
				}
			}
		}
	}

	// ACL propagation can take a little longer on fresh devstack spins, so give it extra headroom.
	const aclPropagationTimeout = 15 * time.Second

	iterations := 15
	if v := os.Getenv("CHAOS_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			iterations = n
		}
	}

	var deadline time.Time
	if v := os.Getenv("CHAOS_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			deadline = time.Now().Add(d)
			// run until deadline (iterations is just a safety cap)
			if iterations < 1_000_000 {
				iterations = 1_000_000
			}
		}
	}

	actionNames := []string{
		"new_public", "overwrite", "nested", "rpc", "grant_read",
		"revoke", "grant_public", "delete", "rapid_acl_flip", "burst_upload",
		"weird_names", "delete_during_download", "acl_change_during_upload", "overwrite_during_download",
	}

mainLoop:
	for i := 0; i < iterations; i++ {
		if deadlinePassed(deadline) {
			t.Logf("Chaos duration reached after %d iterations", i)
			break
		}
		action := rng.Intn(14) // 0 new public, 1 overwrite, 2 nested, 3 rpc, 4 grant read, 5 revoke, 6 grant public, 7 delete, 8 rapid ACL flip, 9 burst upload, 10 weird names, 11 delete during download, 12 ACL change during upload, 13 overwrite during download
		t.Logf("[DEBUG] Iteration %d: action=%d (%s)", i, action, actionNames[action])

		switch action {
		case 0: // new public file
			sender := clients[rng.Intn(len(clients))]
			name := fmt.Sprintf("chaos-%d.bin", i)
			// 50% chance of small file (1-65KB), 50% chance of large file (1-8MB)
			var size int
			if rng.Intn(2) == 0 {
				size = (rng.Intn(64) + 1) * 1024 // 1-65KB
			} else {
				size = (rng.Intn(8) + 1) * 1024 * 1024 // 1-8MB (tests priority limit at 4MB)
			}
			content := GenerateRandomFile(size)
			if err := sender.UploadFile(name, content); err != nil {
				t.Fatalf("upload %s by %s: %v", name, sender.email, err)
			}
			md5 := CalculateMD5(content)
			addFileToExpected(sender.email, name, md5, false)
			// Verify replication to current allowed peers only
			if !verifyReplicationWithDeadline(t, clients, aclState, sender, name, md5, deadline) {
				break mainLoop
			}

		case 1: // overwrite existing - pick random non-RPC file
			if len(expectedFiles) == 0 {
				i--
				continue
			}
			// Collect all unique non-RPC files across all peers
			allFiles := make(map[string]fileInfo)
			for _, peerFiles := range expectedFiles {
				for key, info := range peerFiles {
					if !info.isRPC {
						allFiles[key] = info
					}
				}
			}
			if len(allFiles) == 0 {
				i--
				continue
			}
			// Pick random file
			var fileKeys []string
			for key := range allFiles {
				fileKeys = append(fileKeys, key)
			}
			selectedKey := fileKeys[rng.Intn(len(fileKeys))]
			selectedFile := allFiles[selectedKey]

			var sender *ClientHelper
			for _, c := range clients {
				if c.email == selectedFile.owner {
					sender = c
					break
				}
			}
			if sender == nil {
				t.Fatalf("owner %s not found", selectedFile.owner)
			}

			newContent := GenerateRandomFile((rng.Intn(64)+1)*1024 + 512)
			if err := sender.UploadFile(selectedFile.path, newContent); err != nil {
				t.Fatalf("overwrite %s by %s: %v", selectedFile.path, sender.email, err)
			}
			newMD5 := CalculateMD5(newContent)

			// Update MD5 in expected files for all peers that should have it
			fileKey := fmt.Sprintf("%s/%s", selectedFile.owner, selectedFile.path)
			for peerEmail := range expectedFiles {
				if _, exists := expectedFiles[peerEmail][fileKey]; exists {
					expectedFiles[peerEmail][fileKey] = fileInfo{
						owner: selectedFile.owner,
						path:  selectedFile.path,
						md5:   newMD5,
						isRPC: false,
					}
				}
			}

			// Verify replication to current allowed peers
			if !verifyReplicationWithDeadline(t, clients, aclState, sender, selectedFile.path, newMD5, deadline) {
				break mainLoop
			}

		case 2: // nested public path create/overwrite
			sender := clients[rng.Intn(len(clients))]
			nested := fmt.Sprintf("folder%d/sub%d/request-%d.bin", rng.Intn(3), rng.Intn(3), i)
			content := GenerateRandomFile((rng.Intn(32) + 1) * 1024) // 1–33KB
			if err := sender.UploadFile(nested, content); err != nil {
				t.Fatalf("nested upload %s by %s: %v", nested, sender.email, err)
			}
			md5 := CalculateMD5(content)
			addFileToExpected(sender.email, nested, md5, false)
			// Verify replication to current allowed peers only
			if !verifyReplicationWithDeadline(t, clients, aclState, sender, nested, md5, deadline) {
				break mainLoop
			}
		case 3: // RPC request/response ping-pong
			sender := clients[rng.Intn(len(clients))]
			receiver := clients[rng.Intn(len(clients))]
			for receiver.email == sender.email {
				receiver = clients[rng.Intn(len(clients))]
			}

			app := "chaosapp"
			endpoint := fmt.Sprintf("rpc%d", rng.Intn(3))
			// Ensure RPC endpoints exist
			for _, c := range []*ClientHelper{sender, receiver} {
				if err := c.SetupRPCEndpoint(app, endpoint); err != nil {
					t.Fatalf("setup rpc for %s: %v", c.email, err)
				}
			}

			filename := fmt.Sprintf("rpc-%d.request", i)
			payload := GenerateRandomFile((rng.Intn(16) + 1) * 1024) // 1–17KB
			md5 := CalculateMD5(payload)

			if err := sender.UploadRPCRequest(app, endpoint, filename, payload); err != nil {
				t.Fatalf("rpc send %s->%s: %v", sender.email, receiver.email, err)
			}
			if err := receiver.WaitForRPCRequest(sender.email, app, endpoint, filename, md5, 8*time.Second); err != nil {
				t.Fatalf("rpc recv %s<- %s: %v", receiver.email, sender.email, err)
			}

			// Optionally send a response back (50/50)
			if rng.Intn(2) == 1 {
				respName := fmt.Sprintf("rpc-%d.response", i)
				respPayload := GenerateRandomFile((rng.Intn(16)+1)*1024 + 256)
				respMD5 := CalculateMD5(respPayload)
				if err := receiver.UploadRPCRequest(app, endpoint, respName, respPayload); err != nil {
					t.Fatalf("rpc response %s->%s: %v", receiver.email, sender.email, err)
				}
				if err := sender.WaitForRPCRequest(receiver.email, app, endpoint, respName, respMD5, 8*time.Second); err != nil {
					t.Fatalf("rpc response recv %s<- %s: %v", sender.email, receiver.email, err)
				}
				// RPC files are peer-to-peer, add directly to sender's expected files
				rpcPath := filepath.Join("app_data", app, "rpc", endpoint, respName)
				fileKey := fmt.Sprintf("%s/%s", receiver.email, rpcPath)
				expectedFiles[sender.email][fileKey] = fileInfo{owner: receiver.email, path: rpcPath, md5: respMD5, isRPC: true}
			}

		case 4: // tweak share: grant read to one random peer via ACL and verify
			target := clients[rng.Intn(len(clients))]
			var denied *ClientHelper
			peer := clients[rng.Intn(len(clients))]
			for peer.email == target.email {
				peer = clients[rng.Intn(len(clients))]
			}
			for _, c := range clients {
				if c.email != target.email && c.email != peer.email {
					denied = c
					break
				}
			}
			publicDir := filepath.Join(target.dataDir, "datasites", target.email, "public")
			aclPath := filepath.Join(publicDir, "syft.pub.yaml")
			aclContent := fmt.Sprintf(`terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s','%s']
`, target.email, target.email, peer.email)
			if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
				t.Fatalf("write acl grant for %s to %s: %v", target.email, peer.email, err)
			}

			// Update ACL state - only peer has access now
			updateACLState(target.email, []string{peer.email})

			if !waitForACLPropagationWithDeadline(t, target, clients, aclContent, aclPropagationTimeout, deadline) {
				t.Logf("Deadline reached during ACL propagation")
				break mainLoop
			}

			name := fmt.Sprintf("acl-read-%d.txt", i)
			payload := GenerateRandomFile(2 * 1024)
			if err := target.UploadFile(name, payload); err != nil {
				t.Fatalf("acl-upload %s by %s: %v", name, target.email, err)
			}
			md5 := CalculateMD5(payload)
			if deadlinePassed(deadline) {
				break mainLoop
			}
			if err := peer.WaitForFile(target.email, name, md5, timeoutWithDeadline(8*time.Second, deadline)); err != nil {
				if deadlinePassed(deadline) {
					break mainLoop
				}
				t.Fatalf("peer %s did not receive after grant: %v", peer.email, err)
			}
			if denied != nil && !deadlinePassed(deadline) {
				if err := assertNotReplicated(denied, target.email, name, timeoutWithDeadline(5*time.Second, deadline)); err != nil {
					if !deadlinePassed(deadline) {
						t.Fatalf("denied peer %s unexpectedly received %s: %v", denied.email, name, err)
					}
				}
			}
			// Add file to expected files (will only go to peer based on current ACL state)
			addFileToExpected(target.email, name, md5, false)

		case 5: // revoke share: restrict to owner only and verify others don't see new file
			target := clients[rng.Intn(len(clients))]
			publicDir := filepath.Join(target.dataDir, "datasites", target.email, "public")
			aclPath := filepath.Join(publicDir, "syft.pub.yaml")
			aclContent := fmt.Sprintf(`terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s']
`, target.email, target.email)
			if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
				t.Fatalf("write acl revoke for %s: %v", target.email, err)
			}

			// Update ACL state - no peers have access (owner only)
			updateACLState(target.email, []string{})

			// Don't wait for ACL propagation - owner-only ACL won't be sent to peers
			// Just wait for the ACL to be uploaded to server.
			// On Windows, file watcher + sync cycle takes longer.
			aclUploadWait := 500 * time.Millisecond
			if runtime.GOOS == "windows" {
				aclUploadWait = 3 * time.Second
			}
			time.Sleep(aclUploadWait)

			name := fmt.Sprintf("acl-revoke-%d.txt", i)
			payload := GenerateRandomFile(2 * 1024)
			if err := target.UploadFile(name, payload); err != nil {
				t.Fatalf("acl-revoke upload %s by %s: %v", name, target.email, err)
			}
			md5 := CalculateMD5(payload)
			for _, c := range clients {
				if c.email == target.email {
					continue
				}
				if err := assertNotReplicated(c, target.email, name, 5*time.Second); err != nil {
					t.Fatalf("revoked peer %s unexpectedly received %s: %v", c.email, name, err)
				}
			}
			// Owner only - file won't be added to any peer's expected files
			addFileToExpected(target.email, name, md5, false)

		case 6: // grant public read (*) and verify all receive
			target := clients[rng.Intn(len(clients))]
			publicDir := filepath.Join(target.dataDir, "datasites", target.email, "public")
			aclPath := filepath.Join(publicDir, "syft.pub.yaml")
			aclContent := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['*']
`
			aclContent = fmt.Sprintf(aclContent, target.email)
			if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
				t.Fatalf("write acl public for %s: %v", target.email, err)
			}

			// Update ACL state - all peers have access
			var allPeers []string
			for _, c := range clients {
				if c.email != target.email {
					allPeers = append(allPeers, c.email)
				}
			}
			updateACLState(target.email, allPeers)

			if !waitForACLPropagationWithDeadline(t, target, clients, aclContent, aclPropagationTimeout, deadline) {
				t.Logf("Deadline reached during ACL propagation")
				break mainLoop
			}

			name := fmt.Sprintf("acl-public-%d.txt", i)
			payload := GenerateRandomFile(2 * 1024)
			if err := target.UploadFile(name, payload); err != nil {
				t.Fatalf("acl-public upload %s by %s: %v", name, target.email, err)
			}
			md5 := CalculateMD5(payload)
			for _, c := range clients {
				if c.email == target.email {
					continue
				}
				if deadlinePassed(deadline) {
					break mainLoop
				}
				if err := c.WaitForFile(target.email, name, md5, timeoutWithDeadline(8*time.Second, deadline)); err != nil {
					if deadlinePassed(deadline) {
						break mainLoop
					}
					t.Fatalf("public-read peer %s did not receive %s: %v", c.email, name, err)
				}
			}
			// Add file to expected files (will go to all peers based on current ACL state)
			addFileToExpected(target.email, name, md5, false)

		case 7: // delete random file
			// Collect all unique non-RPC, non-ACL files across all peers that can be deleted
			var deletableFiles []struct {
				key   string
				info  fileInfo
				owner *ClientHelper
			}
			for _, peerFiles := range expectedFiles {
				for key, info := range peerFiles {
					// Skip RPC files and ACL files
					if info.isRPC || strings.Contains(info.path, "syft.pub.yaml") || strings.HasPrefix(info.path, "acl-") {
						continue
					}
					// Find the owner client
					var ownerClient *ClientHelper
					for _, c := range clients {
						if c.email == info.owner {
							ownerClient = c
							break
						}
					}
					if ownerClient != nil {
						// Check if not already in list
						alreadyAdded := false
						for _, df := range deletableFiles {
							if df.key == key {
								alreadyAdded = true
								break
							}
						}
						if !alreadyAdded {
							deletableFiles = append(deletableFiles, struct {
								key   string
								info  fileInfo
								owner *ClientHelper
							}{key, info, ownerClient})
						}
					}
				}
			}

			if len(deletableFiles) == 0 {
				i--
				continue
			}

			// Pick random file to delete
			selected := deletableFiles[rng.Intn(len(deletableFiles))]
			filePath := filepath.Join(selected.owner.publicDir, selected.info.path)

			// Delete the file
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				t.Fatalf("delete %s by %s: %v", selected.info.path, selected.owner.email, err)
			}
			t.Logf("%s deleted %s", selected.owner.email, selected.info.path)

			// Remove from all peers' expected files
			for peerEmail := range expectedFiles {
				delete(expectedFiles[peerEmail], selected.key)
			}

			// Wait a bit for deletion to propagate (if the system supports it)
			// For now, just verify it's gone locally
			time.Sleep(100 * time.Millisecond)

		case 8: // rapid ACL flip-flop (tests notification storms & race conditions)
			target := clients[rng.Intn(len(clients))]
			publicDir := filepath.Join(target.dataDir, "datasites", target.email, "public")
			aclPath := filepath.Join(publicDir, "syft.pub.yaml")

			// Flip between public and owner-only 3 times rapidly
			for flip := 0; flip < 3; flip++ {
				var aclContent string
				var newPeers []string

				if flip%2 == 0 {
					// Public
					aclContent = fmt.Sprintf(`terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['*']
`, target.email)
					for _, c := range clients {
						if c.email != target.email {
							newPeers = append(newPeers, c.email)
						}
					}
				} else {
					// Owner only
					aclContent = fmt.Sprintf(`terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s']
`, target.email, target.email)
					newPeers = []string{}
				}

				if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
					t.Fatalf("write rapid ACL flip %d for %s: %v", flip, target.email, err)
				}
				updateACLState(target.email, newPeers)
				time.Sleep(50 * time.Millisecond) // Very short delay between flips
			}

			// Wait for final state to propagate
			data, _ := os.ReadFile(aclPath)
			if !waitForACLPropagationWithDeadline(t, target, clients, string(data), aclPropagationTimeout, deadline) {
				t.Logf("Deadline reached during rapid ACL propagation")
				break mainLoop
			}

			// Upload a file to verify final state works
			name := fmt.Sprintf("rapid-acl-%d.txt", i)
			payload := GenerateRandomFile(1024)
			if err := target.UploadFile(name, payload); err != nil {
				t.Fatalf("upload after rapid ACL for %s: %v", target.email, err)
			}
			md5 := CalculateMD5(payload)
			addFileToExpected(target.email, name, md5, false)

			// Verify only expected peers got it
			for _, peerEmail := range aclState[target.email] {
				if deadlinePassed(deadline) {
					break mainLoop
				}
				for _, c := range clients {
					if c.email == peerEmail {
						if err := c.WaitForFile(target.email, name, md5, timeoutWithDeadline(8*time.Second, deadline)); err != nil {
							if deadlinePassed(deadline) {
								break mainLoop
							}
							t.Fatalf("rapid-acl peer %s did not receive %s: %v", c.email, name, err)
						}
					}
				}
			}

		case 9: // burst upload (tests queue overflow & batching)
			sender := clients[rng.Intn(len(clients))]
			burstSize := 5 + rng.Intn(6) // 5-10 files

			for j := 0; j < burstSize; j++ {
				name := fmt.Sprintf("burst-%d-%d.bin", i, j)
				content := GenerateRandomFile(100 + rng.Intn(900)) // 100-1000 bytes (tiny files)
				if err := sender.UploadFile(name, content); err != nil {
					t.Fatalf("burst upload %s by %s: %v", name, sender.email, err)
				}
				md5 := CalculateMD5(content)
				addFileToExpected(sender.email, name, md5, false)
			}

			// Wait for all to propagate (skip if deadline passed)
			if !deadlinePassed(deadline) {
				time.Sleep(timeoutWithDeadline(2*time.Second, deadline))
			}

			// Spot-check first and last file
			if deadlinePassed(deadline) {
				break mainLoop
			}
			firstFile := fmt.Sprintf("burst-%d-0.bin", i)
			for _, peerEmail := range aclState[sender.email] {
				if deadlinePassed(deadline) {
					break mainLoop
				}
				for _, c := range clients {
					if c.email == peerEmail {
						fileKey := fmt.Sprintf("%s/%s", sender.email, firstFile)
						if info, exists := expectedFiles[c.email][fileKey]; exists {
							if err := c.WaitForFile(sender.email, firstFile, info.md5, timeoutWithDeadline(5*time.Second, deadline)); err != nil {
								if deadlinePassed(deadline) {
									break mainLoop
								}
								t.Fatalf("burst file %s not received by %s: %v", firstFile, c.email, err)
							}
						}
					}
				}
			}

		case 10: // pathological filenames (tests path handling)
			sender := clients[rng.Intn(len(clients))]
			weirdNames := []string{
				fmt.Sprintf("file with spaces %d.bin", i),
				fmt.Sprintf("unicode-emoji-🎉-%d.bin", i),
				fmt.Sprintf(".hidden-%d.bin", i),
				fmt.Sprintf("UPPERCASE-%d.BIN", i),
				fmt.Sprintf("dots.in.name.%d.bin", i),
			}
			name := weirdNames[rng.Intn(len(weirdNames))]

			content := GenerateRandomFile(1024)
			if err := sender.UploadFile(name, content); err != nil {
				// Some names might fail - that's OK, just log it
				t.Logf("Weird filename '%s' failed (expected): %v", name, err)
				i--
				continue
			}
			md5 := CalculateMD5(content)
			addFileToExpected(sender.email, name, md5, false)

			// Verify replication
			for _, peerEmail := range aclState[sender.email] {
				if deadlinePassed(deadline) {
					break mainLoop
				}
				for _, c := range clients {
					if c.email == peerEmail {
						if err := c.WaitForFile(sender.email, name, md5, timeoutWithDeadline(8*time.Second, deadline)); err != nil {
							if !deadlinePassed(deadline) {
								t.Logf("Weird filename '%s' replication to %s failed: %v", name, c.email, err)
							}
						}
					}
				}
			}

		case 11: // delete during download (race condition test)
			// Upload a large file, start download on peer, delete during download
			sender := clients[rng.Intn(len(clients))]
			// Skip if sender has no peers with access
			if len(aclState[sender.email]) == 0 {
				continue
			}
			receiver := clients[rng.Intn(len(clients))]
			attempts := 0
			for receiver.email == sender.email || !contains(aclState[sender.email], receiver.email) {
				receiver = clients[rng.Intn(len(clients))]
				attempts++
				if attempts > 100 {
					continue // skip this iteration if can't find valid receiver
				}
			}

			name := fmt.Sprintf("delete-race-%d.bin", i)
			size := (2 + rng.Intn(4)) * 1024 * 1024 // 2-6MB
			content := GenerateRandomFile(size)
			md5 := CalculateMD5(content)

			if err := sender.UploadFile(name, content); err != nil {
				t.Fatalf("delete-race upload %s by %s: %v", name, sender.email, err)
			}
			addFileToExpected(sender.email, name, md5, false)

			// Start download in background
			downloadDone := make(chan error, 1)
			go func() {
				downloadDone <- receiver.WaitForFile(sender.email, name, md5, windowsTimeout(15*time.Second))
			}()

			// Delete after short delay (while download may be in progress)
			time.Sleep(time.Duration(100+rng.Intn(300)) * time.Millisecond)
			filePath := filepath.Join(sender.publicDir, name)
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				t.Logf("delete-race: delete %s failed: %v", name, err)
			} else {
				t.Logf("delete-race: %s deleted %s during download", sender.email, name)
			}

			// Remove from expected files
			fileKey := fmt.Sprintf("%s/%s", sender.email, name)
			for peerEmail := range expectedFiles {
				delete(expectedFiles[peerEmail], fileKey)
			}

			// Wait for download to complete or fail
			err := <-downloadDone
			if err == nil {
				t.Logf("delete-race: %s completed download before deletion (valid)", receiver.email)
			} else {
				t.Logf("delete-race: %s download failed (expected): %v", receiver.email, err)
			}

		case 12: // ACL change during upload (TOCTOU race condition test)
			sender := clients[rng.Intn(len(clients))]
			receiver := clients[rng.Intn(len(clients))]
			for receiver.email == sender.email {
				receiver = clients[rng.Intn(len(clients))]
			}

			// Ensure receiver has public ACL initially
			publicDir := filepath.Join(receiver.dataDir, "datasites", receiver.email, "public")
			aclPath := filepath.Join(publicDir, "syft.pub.yaml")
			aclContent := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['*']
`
			aclContent = fmt.Sprintf(aclContent, receiver.email)
			if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
				t.Fatalf("acl-race: write public ACL for %s: %v", receiver.email, err)
			}

			var allPeers []string
			for _, c := range clients {
				if c.email != receiver.email {
					allPeers = append(allPeers, c.email)
				}
			}
			updateACLState(receiver.email, allPeers)
			time.Sleep(500 * time.Millisecond) // Wait for ACL to propagate

			name := fmt.Sprintf("acl-race-%d.bin", i)
			size := (2 + rng.Intn(3)) * 1024 * 1024 // 2-5MB
			content := GenerateRandomFile(size)

			// Start upload in background
			uploadDone := make(chan error, 1)
			go func() {
				targetPath := filepath.Join(sender.dataDir, "datasites", receiver.email, "public", name)
				if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
					uploadDone <- err
					return
				}
				uploadDone <- os.WriteFile(targetPath, content, 0o644)
			}()

			// Change ACL to owner-only during upload
			time.Sleep(time.Duration(100+rng.Intn(200)) * time.Millisecond)
			aclContent = fmt.Sprintf(`terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['%s']
      read: ['%s']
`, receiver.email, receiver.email)
			if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
				t.Logf("acl-race: ACL change failed: %v", err)
			} else {
				t.Logf("acl-race: %s revoked permissions during upload", receiver.email)
			}
			updateACLState(receiver.email, []string{})

			// Wait for upload to complete
			err := <-uploadDone
			if err == nil {
				t.Logf("acl-race: upload completed (TOCTOU - permission checked at start)")
			} else {
				t.Logf("acl-race: upload failed (ideal): %v", err)
			}

		case 13: // overwrite during download (race condition test)
			sender := clients[rng.Intn(len(clients))]
			// Skip if sender has no peers with access
			if len(aclState[sender.email]) == 0 {
				continue
			}
			receiver := clients[rng.Intn(len(clients))]
			attempts := 0
			for receiver.email == sender.email || !contains(aclState[sender.email], receiver.email) {
				receiver = clients[rng.Intn(len(clients))]
				attempts++
				if attempts > 100 {
					continue // skip this iteration if can't find valid receiver
				}
			}

			name := fmt.Sprintf("overwrite-race-%d.bin", i)
			size := (2 + rng.Intn(3)) * 1024 * 1024 // 2-5MB
			v1Content := GenerateRandomFile(size)
			v1MD5 := CalculateMD5(v1Content)

			// Upload version 1
			if err := sender.UploadFile(name, v1Content); err != nil {
				t.Fatalf("overwrite-race v1 upload %s by %s: %v", name, sender.email, err)
			}
			addFileToExpected(sender.email, name, v1MD5, false)

			// Start download of v1 in background
			downloadDone := make(chan error, 1)
			var downloadedMD5 string
			go func() {
				err := receiver.WaitForFile(sender.email, name, v1MD5, windowsTimeout(15*time.Second))
				if err == nil {
					// Read what receiver got
					path := filepath.Join(receiver.dataDir, "datasites", sender.email, "public", name)
					if data, readErr := os.ReadFile(path); readErr == nil {
						downloadedMD5 = CalculateMD5(data)
					}
				}
				downloadDone <- err
			}()

			// Upload version 2 after short delay (while download may be in progress)
			time.Sleep(time.Duration(100+rng.Intn(300)) * time.Millisecond)
			v2Content := GenerateRandomFile(size)
			v2MD5 := CalculateMD5(v2Content)
			if err := sender.UploadFile(name, v2Content); err != nil {
				t.Logf("overwrite-race v2 upload failed: %v", err)
			} else {
				t.Logf("overwrite-race: %s uploaded v2 during download", sender.email)
				// Update expected MD5 to v2
				fileKey := fmt.Sprintf("%s/%s", sender.email, name)
				for peerEmail := range expectedFiles {
					if _, exists := expectedFiles[peerEmail][fileKey]; exists {
						expectedFiles[peerEmail][fileKey] = fileInfo{
							owner: sender.email,
							path:  name,
							md5:   v2MD5,
							isRPC: false,
						}
					}
				}
			}

			// Wait for download to complete
			err := <-downloadDone
			if err == nil {
				if downloadedMD5 == v1MD5 {
					t.Logf("overwrite-race: %s got complete v1 (download before overwrite)", receiver.email)
				} else if downloadedMD5 == v2MD5 {
					t.Logf("overwrite-race: %s got complete v2 (download after overwrite)", receiver.email)
				} else if downloadedMD5 != "" {
					t.Errorf("overwrite-race: %s got corrupted file (mixed v1/v2)!", receiver.email)
				}
			} else {
				t.Logf("overwrite-race: %s download failed: %v", receiver.email, err)
			}
		}
	}

	// Final convergence: verify each client has exactly their expected files
	t.Logf("Verifying final convergence...")
	for _, client := range clients {
		expectedForClient := expectedFiles[client.email]
		t.Logf("%s should have %d files", client.email, len(expectedForClient))

		for fileKey, info := range expectedForClient {
			var err error
			if info.isRPC {
				parts := strings.Split(info.path, string(filepath.Separator))
				if len(parts) < 5 {
					t.Fatalf("invalid rpc path: %s", info.path)
				}
				app := parts[1]
				endpoint := parts[3]
				filename := parts[len(parts)-1]
				err = client.WaitForRPCRequest(info.owner, app, endpoint, filename, info.md5, windowsTimeout(10*time.Second))
			} else {
				err = client.WaitForFile(info.owner, info.path, info.md5, windowsTimeout(10*time.Second))
			}
			if err != nil {
				t.Fatalf("convergence failure: %s@%s missing %s (%s): %v",
					client.email, client.email, fileKey, info.md5, err)
			}
		}
	}
	t.Logf("Convergence verified successfully!")
}

func assertReplicated(t *testing.T, sender *ClientHelper, clients []*ClientHelper, relPath, md5 string, timeout time.Duration) {
	t.Helper()
	for _, c := range clients {
		if c.email == sender.email {
			continue
		}
		if err := c.WaitForFile(sender.email, relPath, md5, timeout); err != nil {
			t.Fatalf("replication to %s for %s failed: %v", c.email, relPath, err)
		}
	}
}

func assertNotReplicated(c *ClientHelper, senderEmail, relPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	path := filepath.Join(c.dataDir, "datasites", senderEmail, "public", relPath)
	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		info, err := os.Stat(path)
		if err == nil {
			fmt.Printf("[DEBUG] assertNotReplicated FAIL: %s unexpectedly received %s from %s (size=%d, attempt=%d)\n",
				c.email, relPath, senderEmail, info.Size(), attempts)
			// Dump parent directory
			parentDir := filepath.Dir(path)
			if entries, err := os.ReadDir(parentDir); err == nil {
				fmt.Printf("[DEBUG] assertNotReplicated: parent dir %s contents:\n", parentDir)
				for _, e := range entries {
					fmt.Printf("[DEBUG]   - %s\n", e.Name())
				}
			}
			return fmt.Errorf("unexpected presence: %s", path)
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("[DEBUG] assertNotReplicated OK: %s did not receive %s from %s after %d checks\n",
		c.email, relPath, senderEmail, attempts)
	return nil
}

func waitForACLPropagation(t *testing.T, owner *ClientHelper, clients []*ClientHelper, content string, timeout time.Duration) {
	t.Helper()
	md5 := CalculateMD5([]byte(content))
	for _, c := range clients {
		if c.email == owner.email {
			continue
		}
		if err := c.WaitForFile(owner.email, "syft.pub.yaml", md5, timeout); err != nil {
			t.Fatalf("ACL propagation to %s from %s failed: %v", c.email, owner.email, err)
		}
	}
}

func waitForACLPropagationWithDeadline(t *testing.T, owner *ClientHelper, clients []*ClientHelper, content string, maxTimeout time.Duration, deadline time.Time) bool {
	t.Helper()
	timeout := maxTimeout
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false // deadline already passed
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	md5 := CalculateMD5([]byte(content))
	for _, c := range clients {
		if c.email == owner.email {
			continue
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return false
		}
		// Debug: log what we're waiting for
		expectedPath := filepath.Join(c.dataDir, "datasites", owner.email, "public", "syft.pub.yaml")
		t.Logf("[DEBUG] ACL propagation: waiting for %s to have %s (md5=%s, timeout=%v)", c.email, expectedPath, md5, timeout)

		if err := c.WaitForFile(owner.email, "syft.pub.yaml", md5, timeout); err != nil {
			// Debug: list what's in the directory
			parentDir := filepath.Dir(expectedPath)
			t.Logf("[DEBUG] ACL propagation failed - checking directory %s", parentDir)
			if entries, readErr := os.ReadDir(parentDir); readErr == nil {
				t.Logf("[DEBUG] Directory contents (%d files):", len(entries))
				for _, e := range entries {
					info, _ := e.Info()
					if info != nil {
						t.Logf("[DEBUG]   - %s (size=%d, mod=%v)", e.Name(), info.Size(), info.ModTime())
					} else {
						t.Logf("[DEBUG]   - %s", e.Name())
					}
				}
			} else {
				t.Logf("[DEBUG] Could not read directory: %v", readErr)
			}
			// Check if file exists but with different content
			if data, readErr := os.ReadFile(expectedPath); readErr == nil {
				actualMD5 := CalculateMD5(data)
				t.Logf("[DEBUG] File exists but MD5 mismatch: expected=%s actual=%s", md5, actualMD5)
				t.Logf("[DEBUG] File content:\n%s", string(data))
			}
			t.Fatalf("ACL propagation to %s from %s failed: %v", c.email, owner.email, err)
		}
		t.Logf("[DEBUG] ACL propagation: %s successfully received ACL from %s", c.email, owner.email)
	}
	return true
}

func timeoutWithDeadline(maxTimeout time.Duration, deadline time.Time) time.Duration {
	if deadline.IsZero() {
		return maxTimeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 100 * time.Millisecond // minimal timeout to allow quick exit
	}
	if remaining < maxTimeout {
		return remaining
	}
	return maxTimeout
}

func deadlinePassed(deadline time.Time) bool {
	return !deadline.IsZero() && time.Now().After(deadline)
}

func verifyReplicationWithDeadline(t *testing.T, clients []*ClientHelper, aclState map[string][]string, sender *ClientHelper, path, md5 string, deadline time.Time) bool {
	t.Helper()
	if deadlinePassed(deadline) {
		return false
	}
	for _, peerEmail := range aclState[sender.email] {
		if deadlinePassed(deadline) {
			return false
		}
		for _, c := range clients {
			if c.email == peerEmail {
				if err := c.WaitForFile(sender.email, path, md5, timeoutWithDeadline(8*time.Second, deadline)); err != nil {
					if deadlinePassed(deadline) {
						return false
					}
					t.Fatalf("replication to %s for %s failed: %v", c.email, path, err)
				}
			}
		}
	}
	return true
}

// forceBroadcastPublicACL rewrites the public ACL to trigger a priority upload even if unchanged.
func forceBroadcastPublicACL(c *ClientHelper) error {
	aclPath := filepath.Join(c.dataDir, "datasites", c.email, "public", "syft.pub.yaml")
	data, err := os.ReadFile(aclPath)
	if err != nil {
		return err
	}
	return os.WriteFile(aclPath, data, 0o644)
}
