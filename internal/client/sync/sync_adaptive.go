package sync

import (
	"sync"
	"time"
)

const (
	// Adaptive sync intervals
	syncIntervalStartup  = 100 * time.Millisecond // Fast peer discovery on startup
	syncIntervalBurst    = 100 * time.Millisecond // During burst activity
	syncIntervalActive   = 100 * time.Millisecond // Regular activity (keep responsive)
	syncIntervalModerate = 500 * time.Millisecond // Occasional activity
	syncIntervalIdle     = 1 * time.Second        // Light idle - first backoff step
	syncIntervalIdle2    = 2 * time.Second        // Medium idle - second backoff step
	syncIntervalIdle3    = 5 * time.Second        // Heavy idle - third backoff step
	syncIntervalDeepIdle = 10 * time.Second       // Deep idle - final backoff (was 30s)

	// Activity detection thresholds
	activityBurstThreshold    = 10               // events in window for burst
	activityActiveThreshold   = 3                // events in window for active
	activityModerateThreshold = 1                // events in window for moderate
	activityWindow            = 10 * time.Second // sliding window for activity detection

	// Idle progression timeouts (exponential backoff)
	idleTimeout1    = 5 * time.Second  // startup → idle (1s)
	idleTimeout2    = 15 * time.Second // idle (1s) → idle2 (2s)
	idleTimeout3    = 30 * time.Second // idle2 (2s) → idle3 (5s)
	deepIdleTimeout = 60 * time.Second // idle3 (5s) → deep idle (10s)

	// Startup detection
	startupDuration = 3 * time.Second // stay in startup mode for initial peer discovery
)

// ActivityLevel represents the current sync activity state
type ActivityLevel int

const (
	ActivityStartup ActivityLevel = iota // Initial fast peer discovery
	ActivityBurst                        // High activity (10+ events in window)
	ActivityActive                       // Regular activity (3+ events)
	ActivityModerate                     // Light activity (1+ events)
	ActivityIdle                         // First idle tier (1s interval)
	ActivityIdle2                        // Second idle tier (2s interval)
	ActivityIdle3                        // Third idle tier (5s interval)
	ActivityDeepIdle                     // Final idle tier (10s interval)
)

func (a ActivityLevel) String() string {
	switch a {
	case ActivityStartup:
		return "startup"
	case ActivityBurst:
		return "burst"
	case ActivityActive:
		return "active"
	case ActivityModerate:
		return "moderate"
	case ActivityIdle:
		return "idle"
	case ActivityIdle2:
		return "idle2"
	case ActivityIdle3:
		return "idle3"
	case ActivityDeepIdle:
		return "deep_idle"
	default:
		return "unknown"
	}
}

// AdaptiveSyncScheduler manages dynamic sync interval based on activity
type AdaptiveSyncScheduler struct {
	mu              sync.RWMutex
	createdAt       time.Time     // When scheduler was created (for startup detection)
	lastActivity    time.Time     // Last activity event timestamp
	eventTimestamps []time.Time   // Recent activity events (sliding window)
	currentLevel    ActivityLevel // Current activity level
}

func NewAdaptiveSyncScheduler() *AdaptiveSyncScheduler {
	now := time.Now()
	return &AdaptiveSyncScheduler{
		createdAt:       now,
		lastActivity:    now,
		eventTimestamps: make([]time.Time, 0, activityBurstThreshold*2),
		currentLevel:    ActivityStartup, // Start in startup mode for fast peer discovery
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
	timeSinceCreation := now.Sub(a.createdAt)

	var newLevel ActivityLevel

	// Startup phase: stay in fast discovery mode for initial period
	if timeSinceCreation < startupDuration {
		newLevel = ActivityStartup
	} else {
		// Normal activity-based detection
		switch {
		case eventCount >= activityBurstThreshold:
			newLevel = ActivityBurst
		case eventCount >= activityActiveThreshold:
			newLevel = ActivityActive
		case eventCount >= activityModerateThreshold:
			newLevel = ActivityModerate
		default:
			// Exponential backoff through idle tiers
			switch {
			case timeSinceActivity < idleTimeout1:
				newLevel = ActivityIdle // 1s interval
			case timeSinceActivity < idleTimeout2:
				newLevel = ActivityIdle2 // 2s interval
			case timeSinceActivity < idleTimeout3:
				newLevel = ActivityIdle3 // 5s interval
			default:
				newLevel = ActivityDeepIdle // 10s interval
			}
		}
	}

	// Update level (sync_engine.go logs changes)
	a.currentLevel = newLevel
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
	case ActivityStartup:
		return syncIntervalStartup // 100ms for fast peer discovery
	case ActivityBurst:
		return syncIntervalBurst // 100ms for high activity
	case ActivityActive:
		return syncIntervalActive // 100ms for regular activity
	case ActivityModerate:
		return syncIntervalModerate // 500ms for light activity
	case ActivityIdle:
		return syncIntervalIdle // 1s (first backoff)
	case ActivityIdle2:
		return syncIntervalIdle2 // 2s (second backoff)
	case ActivityIdle3:
		return syncIntervalIdle3 // 5s (third backoff)
	case ActivityDeepIdle:
		return syncIntervalDeepIdle // 10s (final backoff)
	default:
		return syncIntervalIdle // Fallback to 1s
	}
}

// GetActivityLevel returns the current activity level
func (a *AdaptiveSyncScheduler) GetActivityLevel() ActivityLevel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentLevel
}
