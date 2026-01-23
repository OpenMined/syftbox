package datasitemgr

import (
	"testing"
)

func TestLatencyStats_RecordAndCalculate(t *testing.T) {
	stats := NewLatencyStats("https://test.example.com")

	// Initially empty
	snap := stats.Snapshot()
	if len(snap.Samples) != 0 {
		t.Errorf("expected 0 samples, got %d", len(snap.Samples))
	}
	if snap.AvgMs != 0 {
		t.Errorf("expected avg 0, got %d", snap.AvgMs)
	}
	if snap.ServerURL != "https://test.example.com" {
		t.Errorf("expected server URL https://test.example.com, got %s", snap.ServerURL)
	}

	// Record some samples
	stats.Record(10)
	stats.Record(20)
	stats.Record(30)

	snap = stats.Snapshot()
	if len(snap.Samples) != 3 {
		t.Errorf("expected 3 samples, got %d", len(snap.Samples))
	}
	if snap.AvgMs != 20 { // (10 + 20 + 30) / 3 = 20
		t.Errorf("expected avg 20, got %d", snap.AvgMs)
	}
	if snap.MinMs != 10 {
		t.Errorf("expected min 10, got %d", snap.MinMs)
	}
	if snap.MaxMs != 30 {
		t.Errorf("expected max 30, got %d", snap.MaxMs)
	}
	if snap.LastPingMs == 0 {
		t.Error("expected last ping timestamp to be set")
	}
}

func TestLatencyStats_MaxSamples(t *testing.T) {
	stats := NewLatencyStats("https://test.example.com")

	// Record more than maxLatencySamples
	for i := 0; i < 70; i++ {
		stats.Record(uint64(i))
	}

	snap := stats.Snapshot()
	if len(snap.Samples) != maxLatencySamples {
		t.Errorf("expected %d samples, got %d", maxLatencySamples, len(snap.Samples))
	}
	// Should have samples 10-69 (first 10 dropped)
	if snap.Samples[0] != 10 {
		t.Errorf("expected first sample to be 10, got %d", snap.Samples[0])
	}
	if snap.Samples[59] != 69 {
		t.Errorf("expected last sample to be 69, got %d", snap.Samples[59])
	}
}

func TestLatencyStats_ConcurrentAccess(t *testing.T) {
	stats := NewLatencyStats("https://test.example.com")

	// Run concurrent reads and writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				stats.Record(uint64(n*100 + j))
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				stats.Snapshot()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Should not panic and should have valid data
	snap := stats.Snapshot()
	if len(snap.Samples) > maxLatencySamples {
		t.Errorf("samples exceeded max: %d", len(snap.Samples))
	}
}
