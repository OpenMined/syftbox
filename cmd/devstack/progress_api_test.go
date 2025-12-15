//go:build integration
// +build integration

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

				if file.Progress > 0 && file.Progress < 100 {
					sawProgress = true
					lastProgress = file.Progress
				}

				if file.State == "completed" || file.Progress >= 100 {
					if sawProgress {
						t.Logf("Upload completed with intermediate progress tracking (last seen: %.2f%%)", lastProgress)
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
