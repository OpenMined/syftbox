//go:build integration
// +build integration

package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

type statusHTTPProbe struct {
	Runtime struct {
		HTTP struct {
			BytesSentTotal int64 `json:"bytes_sent_total"`
			BytesRecvTotal int64 `json:"bytes_recv_total"`
		} `json:"http"`
	} `json:"runtime"`
}

func probeHTTPBytes(t *testing.T, baseURL, token string) (sent, recv int64) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("status probe failed for %s: %v", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status probe non-200 for %s: %s", baseURL, resp.Status)
	}
	var snap statusHTTPProbe
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode status probe for %s: %v", baseURL, err)
	}
	return snap.Runtime.HTTP.BytesSentTotal, snap.Runtime.HTTP.BytesRecvTotal
}

func deltaCounter(after, before int64) int64 {
	if after <= before {
		return 0
	}
	return after - before
}

func assertHTTPSentAtLeast(t *testing.T, name string, sent, minBytes int64) {
	t.Helper()
	if sent < minBytes {
		t.Fatalf("%s bytes_sent_total too low: got %d want >= %d", name, sent, minBytes)
	}
	// Guardrail against runaway counters in a single test run.
	if sent > minBytes*20 {
		t.Fatalf("%s bytes_sent_total implausibly high: got %d for transfer of %d", name, sent, minBytes)
	}
	t.Logf("%s HTTP totals: sent=%d", name, sent)
}

func assertHTTPRecvAtLeast(t *testing.T, name string, recv, minBytes int64) {
	t.Helper()
	if recv < minBytes {
		t.Fatalf("%s bytes_recv_total too low: got %d want >= %d", name, recv, minBytes)
	}
	if recv > minBytes*20 {
		t.Fatalf("%s bytes_recv_total implausibly high: got %d for transfer of %d", name, recv, minBytes)
	}
	t.Logf("%s HTTP totals: recv=%d", name, recv)
}
