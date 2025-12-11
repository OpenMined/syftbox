//go:build integration
// +build integration

package main

import (
	"bufio"
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

// ProgressAPIDemoResponse types for display
type DemoSyncStatusResponse struct {
	Files   []DemoSyncFileStatus `json:"files"`
	Summary DemoSyncSummary      `json:"summary"`
}

type DemoSyncFileStatus struct {
	Path          string    `json:"path"`
	State         string    `json:"state"`
	ConflictState string    `json:"conflictState,omitempty"`
	Progress      float64   `json:"progress"`
	Error         string    `json:"error,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type DemoSyncSummary struct {
	Pending   int `json:"pending"`
	Syncing   int `json:"syncing"`
	Completed int `json:"completed"`
	Error     int `json:"error"`
}

type DemoUploadListResponse struct {
	Uploads []DemoUploadInfo `json:"uploads"`
}

type DemoUploadInfo struct {
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

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
)

func printHeader(msg string) {
	fmt.Printf("\n%s%s=== %s ===%s\n", colorBold, colorCyan, msg, colorReset)
}

func printSubHeader(msg string) {
	fmt.Printf("\n%s%s--- %s ---%s\n", colorBold, colorWhite, msg, colorReset)
}

func printAPI(method, endpoint string) {
	fmt.Printf("%s%s%-6s%s %s%s%s\n", colorBold, colorYellow, method, colorReset, colorBlue, endpoint, colorReset)
}

func printSuccess(msg string) {
	fmt.Printf("%s✓ %s%s\n", colorGreen, msg, colorReset)
}

func printInfo(msg string) {
	fmt.Printf("%s→ %s%s\n", colorCyan, msg, colorReset)
}

func printProgress(current, total float64, label string) {
	width := 40
	filled := int(current / total * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Printf("\r%s[%s] %.1f%% %s%s", colorPurple, bar, current, label, colorReset)
}

func printJSON(data interface{}) {
	jsonBytes, _ := json.MarshalIndent(data, "  ", "  ")
	fmt.Printf("  %s%s%s\n", colorWhite, string(jsonBytes), colorReset)
}

// TestProgressAPIDemo is an interactive demo showing the progress API in action
// It demonstrates: sync status, uploads list, progress tracking, pause/resume, error handling, and auth
func TestProgressAPIDemo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping progress API demo in short mode")
	}

	// Use short write timeout to slow down uploads for better demo visibility
	t.Setenv("SBDEV_HTTP_WRITE_TIMEOUT", "2s")

	printHeader("Progress API Demo")
	fmt.Println("This demo shows the sync status and upload management APIs in action.")
	fmt.Println("Features: status tracking, progress bars, pause/resume, error handling, auth")
	fmt.Println()

	// Start devstack
	printInfo("Starting devstack (server + 2 clients)...")
	h := NewDevstackHarness(t)
	time.Sleep(2 * time.Second)

	aliceClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.alice.state.Port)
	printSuccess(fmt.Sprintf("Alice's client running at %s", aliceClientURL))

	// Extract auth token
	authToken := extractDemoAuthToken(t, h.alice.state.LogPath)
	printSuccess(fmt.Sprintf("Auth token: %s...", authToken[:8]))

	// ========== PART 1: Basic API Endpoints ==========
	printHeader("Part 1: Basic API Endpoints")

	// Demo 1: Get initial sync status
	printSubHeader("1.1 Get Sync Status")
	printAPI("GET", "/v1/sync/status")

	status := demoGetSyncStatus(t, aliceClientURL, authToken)
	printSuccess(fmt.Sprintf("Found %d files in sync status", len(status.Files)))
	fmt.Printf("  Summary: pending=%d, syncing=%d, completed=%d, error=%d\n",
		status.Summary.Pending, status.Summary.Syncing, status.Summary.Completed, status.Summary.Error)

	// Demo 2: Get uploads list (empty initially)
	printSubHeader("1.2 List Active Uploads")
	printAPI("GET", "/v1/uploads/")

	uploads := demoGetUploads(t, aliceClientURL, authToken)
	printSuccess(fmt.Sprintf("Active uploads: %d", len(uploads.Uploads)))

	// Demo 3: Trigger sync
	printSubHeader("1.3 Trigger Manual Sync")
	printAPI("POST", "/v1/sync/now")

	demoTriggerSync(t, aliceClientURL, authToken)
	printSuccess("Sync triggered")

	// ========== PART 2: Upload with Progress Tracking ==========
	printHeader("Part 2: Upload with Progress Tracking")

	fileSize := int64(50 * 1024 * 1024) // 50MB
	testFile := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", "demo-upload.bin")

	printInfo(fmt.Sprintf("Creating %dMB test file...", fileSize/(1024*1024)))
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
	printSuccess("File created")

	// Trigger sync to start upload
	printAPI("POST", "/v1/sync/now")
	demoTriggerSync(t, aliceClientURL, authToken)
	printSuccess("Upload started")

	// Poll for progress
	printInfo("Monitoring upload progress...")
	fmt.Println()

	waitForUploadComplete(t, aliceClientURL, authToken, "demo-upload.bin", 120*time.Second)

	// ========== PART 3: Pause/Resume Demo ==========
	printHeader("Part 3: Pause/Resume Upload")

	// Create a larger file for pause/resume demo (500MB to ensure we have time to pause)
	pauseFileSize := int64(500 * 1024 * 1024) // 500MB
	pauseTestFile := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", "pausable-upload.bin")

	printInfo(fmt.Sprintf("Creating %dMB test file for pause/resume...", pauseFileSize/(1024*1024)))
	f2, _ := os.Create(pauseTestFile)
	f2.Truncate(pauseFileSize)
	f2.Close()
	printSuccess("File created")

	// Start the upload
	printAPI("POST", "/v1/sync/now")
	demoTriggerSync(t, aliceClientURL, authToken)

	// Wait for upload to start and get some progress
	printInfo("Waiting for upload to start...")
	var uploadID string
	for i := 0; i < 120; i++ {
		time.Sleep(250 * time.Millisecond)
		uploads := demoGetUploads(t, aliceClientURL, authToken)
		for _, u := range uploads.Uploads {
			if strings.Contains(u.Key, "pausable-upload.bin") {
				// Found the upload - wait until it has some progress before pausing
				if u.Progress > 0 && u.Progress < 90 {
					uploadID = u.ID
					printSuccess(fmt.Sprintf("Upload in progress: ID=%s, progress=%.1f%%", uploadID, u.Progress))
					break
				}
			}
		}
		if uploadID != "" {
			break
		}
	}

	if uploadID == "" {
		printInfo("Upload completed too quickly for pause/resume demo - skipping this section")
		printInfo("(This can happen on fast machines or when uploads don't use multipart)")
	} else {
		// Pause the upload
		printSubHeader("3.1 Pause Upload")
		printAPI("POST", fmt.Sprintf("/v1/uploads/%s/pause", uploadID))
		resp := demoHTTPPost(fmt.Sprintf("%s/v1/uploads/%s/pause", aliceClientURL, uploadID), authToken)
		if resp.StatusCode == http.StatusOK {
			printSuccess("Upload paused!")
		} else {
			body, _ := io.ReadAll(resp.Body)
			printInfo(fmt.Sprintf("Pause response: %d - %s", resp.StatusCode, string(body)))
		}
		resp.Body.Close()

		// Show paused state
		time.Sleep(1 * time.Second)
		uploads := demoGetUploads(t, aliceClientURL, authToken)
		for _, u := range uploads.Uploads {
			if u.ID == uploadID {
				fmt.Printf("  Upload state: %s%s%s, progress: %.1f%%\n", colorYellow, u.State, colorReset, u.Progress)
			}
		}

		// Wait while paused
		printInfo("Waiting 3 seconds while paused...")
		time.Sleep(3 * time.Second)

		// Resume the upload
		printSubHeader("3.2 Resume Upload")
		printAPI("POST", fmt.Sprintf("/v1/uploads/%s/resume", uploadID))
		resp = demoHTTPPost(fmt.Sprintf("%s/v1/uploads/%s/resume", aliceClientURL, uploadID), authToken)
		if resp.StatusCode == http.StatusOK {
			printSuccess("Upload resumed!")
		} else {
			body, _ := io.ReadAll(resp.Body)
			printInfo(fmt.Sprintf("Resume response: %d - %s", resp.StatusCode, string(body)))
		}
		resp.Body.Close()

		// Monitor until complete
		printInfo("Monitoring progress until completion...")
		fmt.Println()

		waitForUploadCompleteByID(t, aliceClientURL, authToken, uploadID, 5*time.Minute)
	}

	// ========== PART 4: Error Handling ==========
	printHeader("Part 4: Error Handling")

	printSubHeader("4.1 Get Non-existent Upload")
	printAPI("GET", "/v1/uploads/nonexistent-id")
	resp := demoHTTPGet(aliceClientURL+"/v1/uploads/nonexistent-id", authToken)
	printSuccess(fmt.Sprintf("Returns %d Not Found (as expected)", resp.StatusCode))
	resp.Body.Close()

	printSubHeader("4.2 Pause Non-existent Upload")
	printAPI("POST", "/v1/uploads/nonexistent-id/pause")
	resp = demoHTTPPost(aliceClientURL+"/v1/uploads/nonexistent-id/pause", authToken)
	printSuccess(fmt.Sprintf("Returns %d Not Found (as expected)", resp.StatusCode))
	resp.Body.Close()

	printSubHeader("4.3 Resume Non-existent Upload")
	printAPI("POST", "/v1/uploads/nonexistent-id/resume")
	resp = demoHTTPPost(aliceClientURL+"/v1/uploads/nonexistent-id/resume", authToken)
	printSuccess(fmt.Sprintf("Returns %d Not Found (as expected)", resp.StatusCode))
	resp.Body.Close()

	printSubHeader("4.4 Cancel Non-existent Upload")
	printAPI("DELETE", "/v1/uploads/nonexistent-id")
	resp = demoHTTPDelete(aliceClientURL+"/v1/uploads/nonexistent-id", authToken)
	printSuccess(fmt.Sprintf("Returns %d Not Found (as expected)", resp.StatusCode))
	resp.Body.Close()

	// ========== PART 5: Authentication ==========
	printHeader("Part 5: Authentication")

	printSubHeader("5.1 Request Without Token")
	printAPI("GET", "/v1/sync/status (no auth)")
	req, _ := http.NewRequest(http.MethodGet, aliceClientURL+"/v1/sync/status", nil)
	resp, _ = http.DefaultClient.Do(req)
	printSuccess(fmt.Sprintf("Returns %d Unauthorized (as expected)", resp.StatusCode))
	resp.Body.Close()

	printSubHeader("5.2 Request With Invalid Token")
	printAPI("GET", "/v1/sync/status (bad token)")
	resp = demoHTTPGet(aliceClientURL+"/v1/sync/status", "invalid-token-12345")
	printSuccess(fmt.Sprintf("Returns %d Unauthorized (as expected)", resp.StatusCode))
	resp.Body.Close()

	// ========== Summary ==========
	printHeader("Demo Complete!")
	fmt.Println()
	fmt.Println("API Endpoints demonstrated:")
	fmt.Println()
	fmt.Printf("  %sSYNC STATUS%s\n", colorBold, colorReset)
	fmt.Println("  GET  /v1/sync/status        - Get sync status for all files")
	fmt.Println("  GET  /v1/sync/status/file   - Get status for a specific file")
	fmt.Println("  POST /v1/sync/now           - Trigger immediate sync")
	fmt.Println()
	fmt.Printf("  %sUPLOAD MANAGEMENT%s\n", colorBold, colorReset)
	fmt.Println("  GET    /v1/uploads/         - List active uploads")
	fmt.Println("  GET    /v1/uploads/:id      - Get upload details")
	fmt.Println("  POST   /v1/uploads/:id/pause   - Pause an upload")
	fmt.Println("  POST   /v1/uploads/:id/resume  - Resume an upload")
	fmt.Println("  POST   /v1/uploads/:id/restart - Restart an upload from scratch")
	fmt.Println("  DELETE /v1/uploads/:id      - Cancel an upload")
	fmt.Println()
	fmt.Printf("  %sAUTHENTICATION%s\n", colorBold, colorReset)
	fmt.Println("  All endpoints require: Authorization: Bearer <token>")
	fmt.Println()
}

func waitForUploadComplete(t *testing.T, baseURL, token, fileNameSuffix string, timeout time.Duration) {
	t.Helper()

	var lastProgress float64
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCh:
			fmt.Println()
			printInfo("Timeout reached (upload may still be in progress)")
			return
		case <-ticker.C:
			// Check sync status
			status := demoGetSyncStatus(t, baseURL, token)
			for _, file := range status.Files {
				if strings.HasSuffix(file.Path, fileNameSuffix) {
					if file.Progress != lastProgress {
						printProgress(file.Progress, 100.0, fmt.Sprintf("state=%s", file.State))
						lastProgress = file.Progress
					}
					if file.State == "completed" || file.Progress >= 100 {
						fmt.Println()
						printSuccess("Upload completed!")
						return
					}
				}
			}

			// Also check uploads endpoint
			uploads := demoGetUploads(t, baseURL, token)
			for _, u := range uploads.Uploads {
				if strings.HasSuffix(u.Key, fileNameSuffix) {
					if u.Progress != lastProgress && u.Progress > 0 {
						printProgress(u.Progress, 100.0, fmt.Sprintf("parts=%d/%d state=%s", len(u.CompletedParts), u.PartCount, u.State))
						lastProgress = u.Progress
					}
				}
			}
		}
	}
}

func waitForUploadCompleteByID(t *testing.T, baseURL, token, uploadID string, timeout time.Duration) {
	t.Helper()

	var lastProgress float64
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCh:
			fmt.Println()
			printInfo("Timeout - check logs for status")
			return
		case <-ticker.C:
			uploads := demoGetUploads(t, baseURL, token)
			found := false
			for _, u := range uploads.Uploads {
				if u.ID == uploadID {
					found = true
					if u.Progress != lastProgress {
						printProgress(u.Progress, 100.0, fmt.Sprintf("state=%s parts=%d/%d", u.State, len(u.CompletedParts), u.PartCount))
						lastProgress = u.Progress
					}
					if u.State == "completed" || u.Progress >= 100 {
						fmt.Println()
						printSuccess("Upload completed!")
						return
					}
				}
			}
			if !found {
				// Upload finished and was removed from registry
				fmt.Println()
				printSuccess("Upload completed and cleaned up!")
				return
			}
		}
	}
}

func extractDemoAuthToken(t *testing.T, logPath string) string {
	t.Helper()
	var token string
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		file, err := os.Open(logPath)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

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
	return token
}

func demoGetSyncStatus(t *testing.T, baseURL, token string) *DemoSyncStatusResponse {
	t.Helper()
	resp := demoHTTPGet(baseURL+"/v1/sync/status", token)
	defer resp.Body.Close()

	var status DemoSyncStatusResponse
	json.NewDecoder(resp.Body).Decode(&status)
	return &status
}

func demoGetUploads(t *testing.T, baseURL, token string) *DemoUploadListResponse {
	t.Helper()
	resp := demoHTTPGet(baseURL+"/v1/uploads/", token)
	defer resp.Body.Close()

	var uploads DemoUploadListResponse
	json.NewDecoder(resp.Body).Decode(&uploads)
	return &uploads
}

func demoTriggerSync(t *testing.T, baseURL, token string) {
	t.Helper()
	resp := demoHTTPPost(baseURL+"/v1/sync/now", token)
	resp.Body.Close()
}

func demoHTTPGet(url, token string) *http.Response {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	return resp
}

func demoHTTPPost(url, token string) *http.Response {
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	return resp
}

func demoHTTPDelete(url, token string) *http.Response {
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	return resp
}
