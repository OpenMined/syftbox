package syftsdk

import "testing"

func TestRecordDownloadedDelta_CountsOnlyForwardProgress(t *testing.T) {
	stats := newHTTPStats()
	var last int64

	recordDownloadedDelta(stats, &last, 10)
	if got := stats.bytesRecv.Load(); got != 10 {
		t.Fatalf("expected 10 bytes recv, got %d", got)
	}

	// same value should not add
	recordDownloadedDelta(stats, &last, 10)
	if got := stats.bytesRecv.Load(); got != 10 {
		t.Fatalf("expected still 10 bytes recv, got %d", got)
	}

	// backwards should not add
	recordDownloadedDelta(stats, &last, 5)
	if got := stats.bytesRecv.Load(); got != 10 {
		t.Fatalf("expected still 10 bytes recv after backwards, got %d", got)
	}

	// forward adds delta
	recordDownloadedDelta(stats, &last, 25)
	if got := stats.bytesRecv.Load(); got != 25 {
		t.Fatalf("expected 25 bytes recv total, got %d", got)
	}
}

