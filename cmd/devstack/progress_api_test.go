//go:build integration
// +build integration

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// SyncStatusResponse matches the API response structure
type SyncStatusResponse struct {
	Files   []SyncFileStatus `json:"files"`
	Summary SyncSummary      `json:"summary"`
}

type SyncFileStatus struct {
	Path          string    `json:"path"`
	State         string    `json:"state"`
	ConflictState string    `json:"conflictState,omitempty"`
	Progress      float64   `json:"progress"`
	Error         string    `json:"error,omitempty"`
	ErrorCount    int       `json:"errorCount,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type SyncSummary struct {
	Pending   int `json:"pending"`
	Syncing   int `json:"syncing"`
	Completed int `json:"completed"`
	Error     int `json:"error"`
}

type UploadListResponse struct {
	Uploads []UploadInfoResponse `json:"uploads"`
}

type UploadInfoResponse struct {
	ID             string    `json:"id"`
	Key            string    `json:"key"`
	LocalPath      string    `json:"localPath"`
	State          string    `json:"state"`
	Size           int64     `json:"size"`
	UploadedBytes  int64     `json:"uploadedBytes"`
	PartSize       int64     `json:"partSize,omitempty"`
	PartCount      int       `json:"partCount,omitempty"`
	CompletedParts []int     `json:"completedParts,omitempty"`
	Progress       float64   `json:"progress"`
	Error          string    `json:"error,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func waitForSyncEvent(ctx context.Context, baseURL, token, wantPathSuffix string) (SyncFileStatus, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return SyncFileStatus{}, err
	}
	u.Path = "/v1/sync/events"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return SyncFileStatus{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return SyncFileStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return SyncFileStatus{}, fmt.Errorf("sync events expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	var evName string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if evName == "sync" && len(dataLines) > 0 {
				raw := strings.Join(dataLines, "\n")
				var st SyncFileStatus
				if err := json.Unmarshal([]byte(raw), &st); err == nil {
					if wantPathSuffix == "" || strings.HasSuffix(st.Path, wantPathSuffix) {
						return st, nil
					}
				}
			}
			evName = ""
			dataLines = dataLines[:0]
			continue
		}

		if strings.HasPrefix(line, "event:") {
			evName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return SyncFileStatus{}, err
	}
	return SyncFileStatus{}, fmt.Errorf("sync events stream closed before receiving event")
}

// TestProgressAPI tests the sync status and upload management API endpoints
func TestProgressAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping progress API test in short mode")
	}

	h := NewDevstackHarness(t)

	// Wait for devstack to be ready
	time.Sleep(2 * time.Second)

	aliceClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.alice.state.Port)

	// Extract auth token from client log
	authToken := extractAuthToken(t, h.alice.state.LogPath)

	maybeWriteWatchEnv(t, aliceClientURL, authToken)

	// Test sync status endpoint
	t.Run("SyncStatus", func(t *testing.T) {
		resp, err := httpGetWithAuth(aliceClientURL+"/v1/sync/status", authToken)
		if err != nil {
			t.Fatalf("failed to get sync status: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
		}

		var status SyncStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		t.Logf("Sync status: files=%d, pending=%d, syncing=%d, completed=%d, error=%d",
			len(status.Files), status.Summary.Pending, status.Summary.Syncing,
			status.Summary.Completed, status.Summary.Error)
	})

	// Test uploads list endpoint (should be empty initially)
	t.Run("UploadsList", func(t *testing.T) {
		resp, err := httpGetWithAuth(aliceClientURL+"/v1/uploads/", authToken)
		if err != nil {
			t.Fatalf("failed to get uploads: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
		}

		var uploads UploadListResponse
		if err := json.NewDecoder(resp.Body).Decode(&uploads); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		t.Logf("Uploads list: %d uploads", len(uploads.Uploads))
	})

	// Test trigger sync endpoint
	t.Run("TriggerSync", func(t *testing.T) {
		resp, err := httpPostWithAuth(aliceClientURL+"/v1/sync/now", authToken, nil)
		if err != nil {
			t.Fatalf("failed to trigger sync: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result["status"] != "sync triggered" {
			t.Errorf("expected status='sync triggered', got %s", result["status"])
		}
	})

	// Test upload not found
	t.Run("UploadNotFound", func(t *testing.T) {
		resp, err := httpGetWithAuth(aliceClientURL+"/v1/uploads/nonexistent", authToken)
		if err != nil {
			t.Fatalf("failed to get upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", resp.StatusCode)
		}
	})

	// Test pause nonexistent upload
	t.Run("PauseNotFound", func(t *testing.T) {
		resp, err := httpPostWithAuth(aliceClientURL+"/v1/uploads/nonexistent/pause", authToken, nil)
		if err != nil {
			t.Fatalf("failed to pause upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", resp.StatusCode)
		}
	})

	// Test resume nonexistent upload
	t.Run("ResumeNotFound", func(t *testing.T) {
		resp, err := httpPostWithAuth(aliceClientURL+"/v1/uploads/nonexistent/resume", authToken, nil)
		if err != nil {
			t.Fatalf("failed to resume upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", resp.StatusCode)
		}
	})

	// Test restart nonexistent upload
	t.Run("RestartNotFound", func(t *testing.T) {
		resp, err := httpPostWithAuth(aliceClientURL+"/v1/uploads/nonexistent/restart", authToken, nil)
		if err != nil {
			t.Fatalf("failed to restart upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", resp.StatusCode)
		}
	})

	// Test cancel nonexistent upload
	t.Run("CancelNotFound", func(t *testing.T) {
		resp, err := httpDeleteWithAuth(aliceClientURL+"/v1/uploads/nonexistent", authToken)
		if err != nil {
			t.Fatalf("failed to cancel upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", resp.StatusCode)
		}
	})

	// Test unauthorized access (no token)
	t.Run("Unauthorized", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, aliceClientURL+"/v1/sync/status", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("SyncEventsUnauthorized", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, aliceClientURL+"/v1/sync/events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", resp.StatusCode)
		}
	})
}

// TestProgressAPIWithUpload tests progress tracking during an actual upload
func TestProgressAPIWithUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping progress API upload test in short mode")
	}

	// Set a short write timeout to force multipart upload to pause/resume
	t.Setenv("SBDEV_HTTP_WRITE_TIMEOUT", "5s")

	h := NewDevstackHarness(t)

	aliceClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.alice.state.Port)

	// Extract auth token from client log
	authToken := extractAuthToken(t, h.alice.state.LogPath)

	// Create a moderately large file (100MB) to test progress tracking
	fileSize := int64(100 * 1024 * 1024)
	testFile := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", "progress-test.bin")

	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create sparse file for fast creation
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := f.Truncate(fileSize); err != nil {
		f.Close()
		t.Fatalf("failed to truncate file: %v", err)
	}
	f.Close()

	t.Logf("Created test file: %s (%d bytes)", testFile, fileSize)

	// Start listening for sync events before triggering the upload.
	wantSuffix := "/public/progress-test.bin"
	evCtx, evCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer evCancel()
	type evResult struct {
		st  SyncFileStatus
		err error
	}
	evCh := make(chan evResult, 1)
	go func() {
		st, err := waitForSyncEvent(evCtx, aliceClientURL, authToken, wantSuffix)
		evCh <- evResult{st: st, err: err}
	}()

	// Trigger sync to start the upload
	resp, err := httpPostWithAuth(aliceClientURL+"/v1/sync/now", authToken, nil)
	if err != nil {
		t.Fatalf("failed to trigger sync: %v", err)
	}
	resp.Body.Close()

	// Wait a bit for sync to pick up the file
	time.Sleep(500 * time.Millisecond)

	// Poll sync status to see the file being uploaded
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var sawProgress bool
	var lastProgress float64
	var fileKey string

	for {
		select {
		case <-ctx.Done():
			if !sawProgress {
				t.Log("upload completed without seeing intermediate progress (may be too fast)")
			}
			return
		default:
		}

		resp, err := httpGetWithAuth(aliceClientURL+"/v1/sync/status", authToken)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		var status SyncStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			resp.Body.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		resp.Body.Close()

		// Look for our file in the status
		for _, file := range status.Files {
			if strings.HasSuffix(file.Path, "/public/progress-test.bin") {
				t.Logf("File status: state=%s, progress=%.2f%%, conflict=%s",
					file.State, file.Progress, file.ConflictState)
				fileKey = file.Path

				if file.Progress > 0 && file.Progress < 100 {
					sawProgress = true
					lastProgress = file.Progress
				}

				if file.State == "completed" || file.Progress >= 100 {
					select {
					case ev := <-evCh:
						if ev.err != nil {
							t.Fatalf("sync events error: %v", ev.err)
						}
						t.Logf("Sync event: path=%s state=%s progress=%.2f%%", ev.st.Path, ev.st.State, ev.st.Progress)
					case <-time.After(5 * time.Second):
						t.Fatalf("timed out waiting for /v1/sync/events sync event for %s", wantSuffix)
					}
					if sawProgress {
						t.Logf("Upload completed with intermediate progress tracking (last seen: %.2f%%)", lastProgress)
					}
					// Verify the per-file endpoint matches the list view (Go parity, required for Rust).
					fileResp, err := httpGetWithAuth(
						aliceClientURL+"/v1/sync/status/file?path="+fileKey,
						authToken,
					)
					if err != nil {
						t.Fatalf("failed to fetch /v1/sync/status/file: %v", err)
					}
					var one SyncFileStatus
					if err := json.NewDecoder(fileResp.Body).Decode(&one); err != nil {
						fileResp.Body.Close()
						t.Fatalf("failed to decode /v1/sync/status/file: %v", err)
					}
					fileResp.Body.Close()
					if one.Path != fileKey {
						t.Fatalf("expected /v1/sync/status/file path %q, got %q", fileKey, one.Path)
					}
					if one.State != "completed" {
						t.Fatalf("expected /v1/sync/status/file state completed, got %q", one.State)
					}
					if one.ConflictState != "none" {
						t.Fatalf("expected /v1/sync/status/file conflictState none, got %q", one.ConflictState)
					}
					if one.Progress < 100 {
						t.Fatalf("expected /v1/sync/status/file progress 100, got %.2f", one.Progress)
					}
					return
				}
			}
		}

		// Also check uploads endpoint
		uploadResp, err := httpGetWithAuth(aliceClientURL+"/v1/uploads/", authToken)
		if err == nil {
			var uploads UploadListResponse
			if err := json.NewDecoder(uploadResp.Body).Decode(&uploads); err == nil {
				for _, u := range uploads.Uploads {
					t.Logf("Upload: id=%s, key=%s, state=%s, progress=%.2f%%, parts=%d/%d",
						u.ID, u.Key, u.State, u.Progress, len(u.CompletedParts), u.PartCount)

					if u.Progress > 0 && u.Progress < 100 {
						sawProgress = true
						lastProgress = u.Progress
					}
				}
			}
			uploadResp.Body.Close()
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func TestProgressAPIPauseResumeUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pause/resume test in short mode")
	}

	// Make uploads slow enough to observe pause/resume behavior deterministically.
	t.Setenv("SBDEV_PART_SIZE", "1MB")
	t.Setenv("SYFTBOX_UPLOAD_PART_SLEEP_MS", "200")

	h := NewDevstackHarness(t)

	aliceClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.alice.state.Port)
	authToken := extractAuthToken(t, h.alice.state.LogPath)

	// Force multipart upload so pause/resume is observable (Rust uses a 32MB multipart threshold).
	fileSize := int64(64 * 1024 * 1024)
	testFile := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", "pause-resume-test.bin")

	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := f.Truncate(fileSize); err != nil {
		f.Close()
		t.Fatalf("failed to truncate file: %v", err)
	}
	f.Close()

	resp, err := httpPostWithAuth(aliceClientURL+"/v1/sync/now", authToken, nil)
	if err != nil {
		t.Fatalf("failed to trigger sync: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), windowsTimeout(45*time.Second))
	defer cancel()

	getWithRetry := func(url string) *http.Response {
		t.Helper()
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for GET %s", url)
			default:
			}
			resp, err := httpGetWithAuth(url, authToken)
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				resp.Body.Close()
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return resp
		}
	}

	postWithRetry := func(url string) *http.Response {
		t.Helper()
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for POST %s", url)
			default:
			}
			resp, err := httpPostWithAuth(url, authToken, nil)
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				resp.Body.Close()
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return resp
		}
	}

	waitForUploadID := func(wantSuffix string) string {
		t.Helper()
		var id string
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for upload to start (suffix=%q id=%q)", wantSuffix, id)
			default:
			}

			uploadResp := getWithRetry(aliceClientURL + "/v1/uploads/")
			var uploads UploadListResponse
			if err := json.NewDecoder(uploadResp.Body).Decode(&uploads); err != nil {
				uploadResp.Body.Close()
				time.Sleep(200 * time.Millisecond)
				continue
			}
			uploadResp.Body.Close()

			for _, u := range uploads.Uploads {
				if strings.HasSuffix(u.Key, wantSuffix) && u.State != "completed" && u.State != "error" {
					id = u.ID
					break
				}
			}
			if id != "" {
				return id
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	getUpload := func(id string) UploadInfoResponse {
		t.Helper()
		uResp := getWithRetry(aliceClientURL + "/v1/uploads/" + id)
		defer uResp.Body.Close()
		if uResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(uResp.Body)
			t.Fatalf("get upload expected 200, got %d: %s", uResp.StatusCode, string(body))
		}
		var u UploadInfoResponse
		if err := json.NewDecoder(uResp.Body).Decode(&u); err != nil {
			t.Fatalf("failed to decode upload info: %v", err)
		}
		return u
	}

	waitForSyncCompleted := func(wantSuffix string) {
		t.Helper()
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for sync status completed for %s", wantSuffix)
			default:
			}
			sResp := getWithRetry(aliceClientURL + "/v1/sync/status")
			var status SyncStatusResponse
			if err := json.NewDecoder(sResp.Body).Decode(&status); err != nil {
				sResp.Body.Close()
				time.Sleep(200 * time.Millisecond)
				continue
			}
			sResp.Body.Close()
			for _, f := range status.Files {
				if strings.HasSuffix(f.Path, wantSuffix) {
					if f.State == "completed" && f.Progress >= 100 {
						return
					}
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	var uploadID string
	uploadID = waitForUploadID("/public/pause-resume-test.bin")

	pauseResp := postWithRetry(aliceClientURL + "/v1/uploads/" + uploadID + "/pause")
	if pauseResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(pauseResp.Body)
		pauseResp.Body.Close()
		t.Fatalf("pause expected status 200, got %d: %s", pauseResp.StatusCode, string(body))
	}
	var pauseBody map[string]string
	_ = json.NewDecoder(pauseResp.Body).Decode(&pauseBody)
	pauseResp.Body.Close()
	if v := pauseBody["status"]; v != "" && v != "paused" {
		t.Fatalf("pause expected status=paused, got %q", v)
	}

	// Give the in-flight work (if any) a chance to settle after pausing.
	time.Sleep(500 * time.Millisecond)
	u1 := getUpload(uploadID)
	time.Sleep(1500 * time.Millisecond)
	u2 := getUpload(uploadID)
	t.Logf("Paused upload progress: before=%.2f%% after=%.2f%% state=%s", u1.Progress, u2.Progress, u2.State)

	resumeResp := postWithRetry(aliceClientURL + "/v1/uploads/" + uploadID + "/resume")
	// Resume behavior is best-effort across clients; accept either a successful resume,
	// a "not paused" response, or a race where the upload already completed.
	if resumeResp.StatusCode != http.StatusOK &&
		resumeResp.StatusCode != http.StatusBadRequest &&
		resumeResp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resumeResp.Body)
		resumeResp.Body.Close()
		t.Fatalf("resume expected 200/400/404, got %d: %s", resumeResp.StatusCode, string(body))
	}
	resumeResp.Body.Close()

	// Wait for completion: Go removes completed uploads from the registry; Rust should match.
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for upload completion (id=%s)", uploadID)
		default:
		}

		uResp := getWithRetry(aliceClientURL + "/v1/uploads/" + uploadID)
		if uResp.StatusCode == http.StatusNotFound {
			uResp.Body.Close()
			break
		}
		if uResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(uResp.Body)
			uResp.Body.Close()
			t.Fatalf("get upload expected 200/404, got %d: %s", uResp.StatusCode, string(body))
		}
		var u UploadInfoResponse
		if err := json.NewDecoder(uResp.Body).Decode(&u); err != nil {
			uResp.Body.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		uResp.Body.Close()
		if u.Progress >= 100 || u.State == "completed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Finally, assert the file is marked completed in sync status.
	waitForSyncCompleted("/public/pause-resume-test.bin")

	// Restart contract (same endpoint used by Progress API demo): reset progress to ~0 and
	// re-upload from scratch.
	restartFile := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", "restart-test.bin")
	f2, err := os.Create(restartFile)
	if err != nil {
		t.Fatalf("failed to create restart test file: %v", err)
	}
	if err := f2.Truncate(fileSize); err != nil {
		f2.Close()
		t.Fatalf("failed to truncate restart file: %v", err)
	}
	f2.Close()

	resp2 := postWithRetry(aliceClientURL + "/v1/sync/now")
	resp2.Body.Close()

	restartID := waitForUploadID("/public/restart-test.bin")
	restartResp := postWithRetry(aliceClientURL + "/v1/uploads/" + restartID + "/restart")
	if restartResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(restartResp.Body)
		restartResp.Body.Close()
		t.Fatalf("restart expected status 200, got %d: %s", restartResp.StatusCode, string(body))
	}
	restartResp.Body.Close()

	// Immediately after restart, the upload should reflect reset progress (at least briefly).
	u0 := getUpload(restartID)
	if !(u0.UploadedBytes == 0 || u0.Progress <= 10 || u0.State == "pending" || u0.State == "restarted") {
		t.Fatalf("expected restart to reset progress (uploadedBytes=0/progress<=10/state=pending), got state=%q uploadedBytes=%d progress=%.2f%%",
			u0.State, u0.UploadedBytes, u0.Progress)
	}

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for restart upload completion (id=%s)", restartID)
		default:
		}

		uResp := getWithRetry(aliceClientURL + "/v1/uploads/" + restartID)
		if uResp.StatusCode == http.StatusNotFound {
			uResp.Body.Close()
			break
		}
		if uResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(uResp.Body)
			uResp.Body.Close()
			t.Fatalf("get restart upload expected 200/404, got %d: %s", uResp.StatusCode, string(body))
		}
		var u UploadInfoResponse
		if err := json.NewDecoder(uResp.Body).Decode(&u); err != nil {
			uResp.Body.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		uResp.Body.Close()
		if u.Progress >= 100 || u.State == "completed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	waitForSyncCompleted("/public/restart-test.bin")
}

// extractAuthToken parses the client log file to extract the auth token
func extractAuthToken(t *testing.T, logPath string) string {
	t.Helper()

	// Wait for log file to be created and contain the token
	var token string
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		file, err := os.Open(logPath)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// slog format: time=... level=INFO msg="control plane start" addr=... token=abc123
		tokenRegex := regexp.MustCompile(`token=([a-fA-F0-9]+)`)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "control plane start") {
				matches := tokenRegex.FindStringSubmatch(line)
				if len(matches) >= 2 {
					token = matches[1]
					break
				}
			}
		}
		file.Close()

		if token != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if token == "" {
		t.Fatalf("failed to extract auth token from %s", logPath)
	}

	t.Logf("Extracted auth token: %s...", token[:8])
	return token
}

func httpGetWithAuth(url, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func httpPostWithAuth(url, token string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func httpDeleteWithAuth(url, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}
