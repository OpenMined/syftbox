package datasitemgr

import (
	"sync"
	"time"
)

const maxLatencySamples = 60

// LatencyStats stores server latency measurements
type LatencyStats struct {
	mu          sync.RWMutex
	samples     []uint64
	serverURL   string
	lastPingMs  uint64
}

// NewLatencyStats creates a new latency stats tracker
func NewLatencyStats(serverURL string) *LatencyStats {
	return &LatencyStats{
		samples:   make([]uint64, 0, maxLatencySamples),
		serverURL: serverURL,
	}
}

// Record adds a latency sample
func (l *LatencyStats) Record(latencyMs uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.samples) >= maxLatencySamples {
		l.samples = l.samples[1:]
	}
	l.samples = append(l.samples, latencyMs)
	l.lastPingMs = uint64(time.Now().UnixMilli())
}

// Snapshot returns a copy of the current stats
func (l *LatencyStats) Snapshot() LatencySnapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()

	samples := make([]uint64, len(l.samples))
	copy(samples, l.samples)

	var avg, min, max uint64
	if len(samples) > 0 {
		var sum uint64
		min = samples[0]
		max = samples[0]
		for _, s := range samples {
			sum += s
			if s < min {
				min = s
			}
			if s > max {
				max = s
			}
		}
		avg = sum / uint64(len(samples))
	}

	return LatencySnapshot{
		ServerURL:  l.serverURL,
		Samples:    samples,
		AvgMs:      avg,
		MinMs:      min,
		MaxMs:      max,
		LastPingMs: l.lastPingMs,
	}
}

// LatencySnapshot is the JSON response for latency stats
type LatencySnapshot struct {
	ServerURL  string   `json:"serverUrl"`
	Samples    []uint64 `json:"samples"`
	AvgMs      uint64   `json:"avgMs"`
	MinMs      uint64   `json:"minMs"`
	MaxMs      uint64   `json:"maxMs"`
	LastPingMs uint64   `json:"lastPingMs"`
}
