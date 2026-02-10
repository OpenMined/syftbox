package sync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAdaptiveSyncScheduler_StartupInterval(t *testing.T) {
	s := NewAdaptiveSyncScheduler()
	assert.Equal(t, ActivityStartup, s.GetActivityLevel())
	assert.Equal(t, syncIntervalStartup, s.GetSyncInterval())
}

func TestAdaptiveSyncScheduler_ActivityLevelsByEventCount(t *testing.T) {
	s := NewAdaptiveSyncScheduler()
	now := time.Now()

	// Bypass startup.
	s.mu.Lock()
	s.createdAt = now.Add(-startupDuration * 2)
	s.mu.Unlock()

	// Moderate: 1 event in window.
	s.RecordActivity()
	assert.Equal(t, ActivityModerate, s.GetActivityLevel())
	assert.Equal(t, syncIntervalModerate, s.GetSyncInterval())

	// Active: >=3 events.
	s.RecordActivity()
	s.RecordActivity()
	assert.Equal(t, ActivityActive, s.GetActivityLevel())
	assert.Equal(t, syncIntervalActive, s.GetSyncInterval())

	// Burst: >=10 events.
	for i := 0; i < activityBurstThreshold; i++ {
		s.RecordActivity()
	}
	assert.Equal(t, ActivityBurst, s.GetActivityLevel())
	assert.Equal(t, syncIntervalBurst, s.GetSyncInterval())
}

func TestAdaptiveSyncScheduler_IdleBackoff(t *testing.T) {
	s := NewAdaptiveSyncScheduler()
	now := time.Now()

	s.mu.Lock()
	s.createdAt = now.Add(-startupDuration * 2)
	// No events in window, simulate idle tiers by lastActivity age.
	s.lastActivity = now.Add(-time.Second) // < idleTimeout1 => Idle
	s.eventTimestamps = nil
	s.mu.Unlock()

	assert.Equal(t, syncIntervalIdle, s.GetSyncInterval())
	assert.Equal(t, ActivityIdle, s.GetActivityLevel())

	// Move to Idle2.
	s.mu.Lock()
	s.lastActivity = now.Add(-idleTimeout1 - time.Second) // between idleTimeout1 and idleTimeout2
	s.mu.Unlock()
	assert.Equal(t, syncIntervalIdle2, s.GetSyncInterval())
	assert.Equal(t, ActivityIdle2, s.GetActivityLevel())

	// Move to Idle3.
	s.mu.Lock()
	s.lastActivity = now.Add(-idleTimeout2 - time.Second) // between idleTimeout2 and idleTimeout3
	s.mu.Unlock()
	assert.Equal(t, syncIntervalIdle3, s.GetSyncInterval())
	assert.Equal(t, ActivityIdle3, s.GetActivityLevel())

	// Move to DeepIdle.
	s.mu.Lock()
	s.lastActivity = now.Add(-idleTimeout3 - time.Second) // >= idleTimeout3
	s.mu.Unlock()
	assert.Equal(t, syncIntervalDeepIdle, s.GetSyncInterval())
	assert.Equal(t, ActivityDeepIdle, s.GetActivityLevel())
}
