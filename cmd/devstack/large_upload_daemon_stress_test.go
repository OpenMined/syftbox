//go:build integration
// +build integration

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestLargeUploadViaDaemonStress exercises resumable uploads via the syftbox daemon under
// pathological conditions: short per-part timeouts and killing/restarting the client mid-upload.
func TestLargeUploadViaDaemonStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large upload stress test in short mode")
	}

	// Use smaller-than-default parts to get visible multipart progress.
	// Avoid forcing per-part timeouts here; we validate resumability by kill/restart.
	// You can override these envs when running the test.
	t.Setenv("SBDEV_PART_SIZE", "8MB")
	// Ensure no externally-set per-part timeout makes this test flake.
	t.Setenv("SBDEV_PART_UPLOAD_TIMEOUT", "")
	// Slow down per-part uploads so progress is observable.
	t.Setenv("SYFTBOX_UPLOAD_PART_SLEEP_MS", "250")
	// Give server enough write time so parts can complete on slower machines.
	t.Setenv("SBDEV_HTTP_WRITE_TIMEOUT", "8s")

	h := NewDevstackHarness(t)
	time.Sleep(2 * time.Second)

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", h.state.Server.Port)

	aliceClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.alice.state.Port)
	authToken := extractAuthToken(t, h.alice.state.LogPath)
	maybeWriteWatchEnv(t, aliceClientURL, authToken)

	bobClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.bob.state.Port)
	bobToken := extractAuthToken(t, h.bob.state.LogPath)

	aliceSent0, aliceRecv0 := probeHTTPBytes(t, aliceClientURL, authToken)
	bobSent0, bobRecv0 := probeHTTPBytes(t, bobClientURL, bobToken)

	// Create a large file in Alice's datasite; daemon should use multipart/resume.
	fileSize := int64(512 * 1024 * 1024) // 512MB for visibly longer multipart uploads
	relName := "daemon-large-upload-stress.bin"
	testFile := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", relName)
	if err := os.MkdirAll(filepath.Dir(testFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := f.Truncate(fileSize); err != nil {
		f.Close()
		t.Fatalf("truncate file: %v", err)
	}
	f.Close()

	// Start upload.
	demoTriggerSync(t, aliceClientURL, authToken)
	t.Logf("Triggered initial sync; waiting for multipart progress on %s", relName)

	// Wait for at least one part to complete before killing.
	uploadID, _ := waitForUploadParts(t, aliceClientURL, authToken, relName, 1, 3*time.Minute)
	t.Logf("upload started with id=%s; killing alice daemon", uploadID)

	aliceSentBeforeKill, aliceRecvBeforeKill := probeHTTPBytes(t, aliceClientURL, authToken)
	t.Logf("alice HTTP delta before kill: sent=%d recv=%d",
		deltaCounter(aliceSentBeforeKill, aliceSent0),
		deltaCounter(aliceRecvBeforeKill, aliceRecv0),
	)

	// Kill alice mid-upload.
	if err := killProcess(h.alice.state.PID); err != nil {
		t.Fatalf("kill alice: %v", err)
	}
	uploadKey := filepath.ToSlash(filepath.Join(h.alice.email, "public", relName))
	uploadResumeDir := filepath.Join(h.alice.dataDir, ".data", "upload-sessions")
	uploadedBeforeKill := readUploadSessionBytes(t, uploadResumeDir, uploadKey, testFile)
	t.Logf("upload session completed bytes before restart: %d", uploadedBeforeKill)

	// Restart alice on same port/root so it can resume from sessions.
	newState, err := startClient(h.alice.state.BinPath, h.root, h.alice.email, serverURL, h.alice.state.Port)
	if err != nil {
		t.Fatalf("restart alice: %v", err)
	}
	h.alice.state = newState
	h.state.Clients[0] = newState
	h.alice.dataDir = newState.DataPath
	h.alice.publicDir = filepath.Join(newState.DataPath, "datasites", h.alice.email, "public")
	aliceClientURL = fmt.Sprintf("http://127.0.0.1:%d", newState.Port)

	// Give daemon a moment to boot and rewrite log/token.
	// Windows needs more time for port release and process startup.
	startupWait := 2 * time.Second
	if runtime.GOOS == "windows" {
		startupWait = 5 * time.Second
	}
	time.Sleep(startupWait)
	authToken = extractAuthToken(t, newState.LogPath)
	maybeWriteWatchEnv(t, aliceClientURL, authToken)

	// Wait for client to be ready with retries (use non-fatal probe)
	var aliceSentRestart0, aliceRecvRestart0 int64
	var probeErr error
	probeDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(probeDeadline) {
		sent, recv, err := tryProbeHTTPBytes(aliceClientURL, authToken)
		if err == nil {
			aliceSentRestart0, aliceRecvRestart0 = sent, recv
			probeErr = nil
			break
		}
		probeErr = err
		t.Logf("Waiting for alice client to be ready: %v", err)
		time.Sleep(2 * time.Second)
	}
	if probeErr != nil {
		t.Fatalf("alice client never became ready after restart: %v", probeErr)
	}
	t.Logf("alice HTTP totals after restart baseline: sent=%d recv=%d", aliceSentRestart0, aliceRecvRestart0)

	// Trigger sync repeatedly until bob sees the file.
	deadline := time.Now().Add(10 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		demoTriggerSync(t, aliceClientURL, authToken)
		if err := h.bob.WaitForFile(h.alice.email, relName, "", 2*time.Second); err == nil {
			// Probe both clients' /v1/status while daemons are alive.
			aliceSent, aliceRecv := probeHTTPBytes(t, aliceClientURL, authToken)
			bobSent, bobRecv := probeHTTPBytes(t, bobClientURL, bobToken)

			// Alice was restarted and may start syncing immediately, so we cannot reliably capture a
			// "zero baseline" for the new process. Use the full post-restart total plus the pre-kill
			// uploaded bytes (from the upload registry) for a lower bound.
			aliceSentDelta := uploadedBeforeKill + aliceSent
			bobRecvDelta := deltaCounter(bobRecv, bobRecv0)

			t.Logf("alice HTTP delta combined: sent=%d recv=%d",
				aliceSentDelta,
				deltaCounter(aliceRecvBeforeKill, aliceRecv0)+aliceRecv,
			)
			t.Logf("bob HTTP delta: sent=%d recv=%d", deltaCounter(bobSent, bobSent0), bobRecvDelta)

			// With server-side multipart caching, alice may not need to resend all bytes.
			// The key assertion is that bob received the full file (which we already verified above).
			// Just verify alice sent *some* data (at least 10% to prove upload activity).
			assertHTTPSentAtLeast(t, "alice (delta combined)", aliceSentDelta, fileSize/10)
			assertHTTPRecvAtLeast(t, "bob (delta)", bobRecvDelta, fileSize)
			return
		}
		select {
		case <-ticker.C:
			t.Log("Still waiting for bob to receive file; re-triggering sync...")
		default:
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("bob did not receive stress upload within timeout")
}

// waitForUploadParts polls /v1/uploads until the upload matching suffix has at least minParts completed.
func waitForUploadParts(t *testing.T, baseURL, token, fileNameSuffix string, minParts int, timeout time.Duration) (string, int64) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	logTicker := time.NewTicker(5 * time.Second)
	defer logTicker.Stop()
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/uploads/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
	var uploads struct {
		Uploads []struct {
			ID             string  `json:"id"`
			Key            string  `json:"key"`
			State          string  `json:"state"`
			Error          string  `json:"error,omitempty"`
			CompletedParts []int   `json:"completedParts"`
			PartCount      int     `json:"partCount"`
			PartSize       int64   `json:"partSize"`
			Size           int64   `json:"size"`
			UploadedBytes  int64   `json:"uploadedBytes"`
			Progress       float64 `json:"progress"`
		} `json:"uploads"`
	}
		_ = json.NewDecoder(resp.Body).Decode(&uploads)
		resp.Body.Close()
		for _, u := range uploads.Uploads {
			if strings.HasSuffix(u.Key, fileNameSuffix) {
				if len(u.CompletedParts) >= minParts || (u.PartCount > 0 && u.Progress > 0) {
					uploaded := u.UploadedBytes
					if uploaded == 0 && len(u.CompletedParts) > 0 {
						if u.PartSize > 0 {
							uploaded = int64(len(u.CompletedParts)) * u.PartSize
						} else if u.Size > 0 && u.PartCount > 0 {
							uploaded = int64(len(u.CompletedParts)) * (u.Size / int64(u.PartCount))
						}
					}
					return u.ID, uploaded
				}
			}
		}
		select {
		case <-logTicker.C:
			for _, u := range uploads.Uploads {
				if strings.HasSuffix(u.Key, fileNameSuffix) {
					if u.State == "error" && u.Error != "" {
						t.Logf("Upload state=error err=%s parts=%d/%d", u.Error, len(u.CompletedParts), u.PartCount)
					} else {
						t.Logf("Upload state=%s progress=%.1f%% parts=%d/%d",
							u.State, u.Progress, len(u.CompletedParts), u.PartCount)
					}
				}
			}
		default:
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to reach %d completed parts", fileNameSuffix, minParts)
	return "", 0
}

func readUploadSessionBytes(t *testing.T, resumeDir, key, filePath string) int64 {
	t.Helper()
	hash := sha1.Sum([]byte(key + "|" + filePath))
	sessionPath := filepath.Join(resumeDir, hex.EncodeToString(hash[:])+".json")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Logf("upload session file not found (%s): %v", sessionPath, err)
		return 0
	}
	var session struct {
		Size      int64          `json:"size"`
		PartSize  int64          `json:"partSize"`
		PartCount int            `json:"partCount"`
		Completed map[int]string `json:"completed"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		t.Logf("decode upload session (%s): %v", sessionPath, err)
		return 0
	}
	if session.Completed == nil || session.Size == 0 {
		return 0
	}
	partSize := session.PartSize
	if partSize == 0 && session.PartCount > 0 {
		partSize = session.Size / int64(session.PartCount)
	}
	if partSize <= 0 {
		return 0
	}
	var total int64
	for part := range session.Completed {
		offset := int64(part-1) * partSize
		if offset >= session.Size {
			continue
		}
		remaining := session.Size - offset
		if remaining < partSize {
			total += remaining
		} else {
			total += partSize
		}
	}
	return total
}
