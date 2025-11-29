package sync

import (
	"sync"
	"time"
)

const (
	// Adaptive sync intervals
	syncIntervalBurst    = 500 * time.Millisecond // During burst activity
	syncIntervalActive   = 1 * time.Second        // Regular activity
	syncIntervalModerate = 2500 * time.Millisecond // Occasional activity
	syncIntervalIdle     = 5 * time.Second        // Default (current)
	syncIntervalDeepIdle = 30 * time.Second       // Extended idle period

	// Activity detection thresholds
	activityBurstThreshold    = 10               // events in window for burst
	activityActiveThreshold   = 3                // events in window for active
	activityModerateThreshold = 1                // events in window for moderate
	activityWindow            = 10 * time.Second // sliding window for activity detection
	deepIdleTimeout           = 5 * time.Minute  // time before deep idle
)

// ActivityLevel represents the current sync activity state
type ActivityLevel int

const (
	ActivityDeepIdle ActivityLevel = iota
	ActivityIdle
	ActivityModerate
	ActivityActive
	ActivityBurst
)

func (a ActivityLevel) String() string {
	switch a {
	case ActivityBurst:
		return "burst"
	case ActivityActive:
		return "active"
	case ActivityModerate:
		return "moderate"
	case ActivityIdle:
		return "idle"
	case ActivityDeepIdle:
		return "deep_idle"
	default:
		return "unknown"
	}
}

// AdaptiveSyncScheduler manages dynamic sync interval based on activity
type AdaptiveSyncScheduler struct {
	mu              sync.RWMutex
	lastActivity    time.Time
	eventTimestamps []time.Time
	currentLevel    ActivityLevel
}

func NewAdaptiveSyncScheduler() *AdaptiveSyncScheduler {
	return &AdaptiveSyncScheduler{
		lastActivity:    time.Now(),
		eventTimestamps: make([]time.Time, 0, activityBurstThreshold*2),
		currentLevel:    ActivityIdle,
	}
}

// RecordActivity registers an activity event (file change, WebSocket message, etc.)
func (a *AdaptiveSyncScheduler) RecordActivity() {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	a.lastActivity = now

	// Add new timestamp
	a.eventTimestamps = append(a.eventTimestamps, now)

	// Remove timestamps outside the activity window
	cutoff := now.Add(-activityWindow)
	validIdx := 0
	for i, ts := range a.eventTimestamps {
		if ts.After(cutoff) {
			validIdx = i
			break
		}
	}
	a.eventTimestamps = a.eventTimestamps[validIdx:]

	// Recalculate activity level
	a.updateActivityLevel(now)
}

// updateActivityLevel calculates current activity level based on recent events
func (a *AdaptiveSyncScheduler) updateActivityLevel(now time.Time) {
	eventCount := len(a.eventTimestamps)
	timeSinceActivity := now.Sub(a.lastActivity)

	var newLevel ActivityLevel
	switch {
	case eventCount >= activityBurstThreshold:
		newLevel = ActivityBurst
	case eventCount >= activityActiveThreshold:
		newLevel = ActivityActive
	case eventCount >= activityModerateThreshold:
		newLevel = ActivityModerate
	case timeSinceActivity < deepIdleTimeout:
		newLevel = ActivityIdle
	default:
		newLevel = ActivityDeepIdle
	}

	// Log level changes
	if newLevel != a.currentLevel {
		a.currentLevel = newLevel
	}
}

// GetSyncInterval returns the appropriate sync interval for current activity level
func (a *AdaptiveSyncScheduler) GetSyncInterval() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Update activity level based on time elapsed
	now := time.Now()
	a.mu.RUnlock()
	a.mu.Lock()
	a.updateActivityLevel(now)
	a.mu.Unlock()
	a.mu.RLock()

	switch a.currentLevel {
	case ActivityBurst:
		return syncIntervalBurst
	case ActivityActive:
		return syncIntervalActive
	case ActivityModerate:
		return syncIntervalModerate
	case ActivityDeepIdle:
		return syncIntervalDeepIdle
	default:
		return syncIntervalIdle
	}
}

// GetActivityLevel returns the current activity level
func (a *AdaptiveSyncScheduler) GetActivityLevel() ActivityLevel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentLevel
}
