//go:build integration
// +build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type subscriptionsAPIResponse struct {
	Path   string `json:"path"`
	Config struct {
		Version  int               `json:"version"`
		Defaults map[string]string `json:"defaults"`
		Rules    []struct {
			Action   string `json:"action"`
			Datasite string `json:"datasite"`
			Path     string `json:"path"`
		} `json:"rules"`
	} `json:"config"`
}

type subscriptionsEffectiveResponse struct {
	Files []struct {
		Path    string `json:"path"`
		Action  string `json:"action"`
		Allowed bool   `json:"allowed"`
	} `json:"files"`
}

// TestSubscriptionsAPI verifies control plane endpoints for subscriptions.
func TestSubscriptionsAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := NewDevstackHarness(t)

	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	// Upload a file from alice so bob can discover it.
	filename := "sub-api.txt"
	if err := h.alice.UploadFile(filename, []byte("sub api test")); err != nil {
		t.Fatalf("alice upload failed: %v", err)
	}

	time.Sleep(2 * time.Second)

	bobClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.bob.state.Port)
	authToken := extractAuthToken(t, h.bob.state.LogPath)

	t.Run("GetSubscriptions", func(t *testing.T) {
		resp, err := httpGetWithAuth(bobClientURL+"/v1/subscriptions", authToken)
		if err != nil {
			t.Fatalf("get subscriptions: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
		var out subscriptionsAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode subscriptions: %v", err)
		}
		if out.Config.Defaults["action"] == "" {
			t.Fatalf("expected defaults.action to be set")
		}
	})

	t.Run("AddRule", func(t *testing.T) {
		payload := map[string]any{
			"rule": map[string]any{
				"action":   "allow",
				"datasite": "alice@example.com",
				"path":     "public/**",
			},
		}
		raw, _ := json.Marshal(payload)
		resp, err := httpPostWithAuth(bobClientURL+"/v1/subscriptions/rules", authToken, bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("post rule: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Effective", func(t *testing.T) {
		resp, err := httpGetWithAuth(bobClientURL+"/v1/subscriptions/effective", authToken)
		if err != nil {
			t.Fatalf("get effective: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
		var out subscriptionsEffectiveResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode effective: %v", err)
		}
		wantSuffix := "alice@example.com/public/" + filename
		found := false
		for _, f := range out.Files {
			if strings.HasSuffix(f.Path, wantSuffix) {
				if f.Action != "allow" || !f.Allowed {
					t.Fatalf("expected allow for %s, got action=%s allowed=%v", f.Path, f.Action, f.Allowed)
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected effective entry for %s", wantSuffix)
		}
	})

	t.Run("DeleteRule", func(t *testing.T) {
		url := bobClientURL + "/v1/subscriptions/rules?datasite=alice@example.com&path=public/**"
		resp, err := httpDeleteWithAuth(url, authToken)
		if err != nil {
			t.Fatalf("delete rule: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Refresh", func(t *testing.T) {
		resp, err := httpPostWithAuth(bobClientURL+"/v1/sync/refresh?path=alice@example.com/public/"+filename, authToken, nil)
		if err != nil {
			t.Fatalf("refresh sync: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
	})
}
