package sync

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	progressMin         = 0.0
	progressMax         = 100.0
	syncEventBufferSize = 16
)

// SyncState represents the state of a sync operation
type SyncState string

const (
	SyncStatePending   SyncState = "pending"
	SyncStateSyncing   SyncState = "syncing"
	SyncStateCompleted SyncState = "completed"
	SyncStateError     SyncState = "error"
)

// ConflictState represents the condition of a file
type ConflictState string

const (
	ConflictStateNone       ConflictState = "none"
	ConflictStateConflicted ConflictState = "conflicted"
	ConflictStateRejected   ConflictState = "rejected"
)

// PathStatus represents the complete status of a file
type PathStatus struct {
	SyncState     SyncState
	ConflictState ConflictState
	Progress      float64
	Error         error
	ErrorCount    int
	LastUpdated   time.Time
}

func (s *PathStatus) String() string {
	return fmt.Sprintf("SyncState: %s, ConflictState: %s, Progress: %f, Error: %v, ErrorCount: %d", s.SyncState, s.ConflictState, s.Progress, s.Error, s.ErrorCount)
}

// SyncStatusEvent represents a status change event for broadcasting
type SyncStatusEvent struct {
	Path   SyncPath
	Status *PathStatus
}

// SyncStatus manages the status of file synchronization operations
type SyncStatus struct {
	files map[SyncPath]*PathStatus
	mu    sync.RWMutex

	// Event broadcasting (for future control plane API)
	eventSubs []chan *SyncStatusEvent
	eventMu   sync.RWMutex
}

func NewSyncStatus() *SyncStatus {
	return &SyncStatus{
		files:     make(map[SyncPath]*PathStatus),
		eventSubs: make([]chan *SyncStatusEvent, 0),
	}
}

// Subscribe returns a channel for receiving sync status events
func (s *SyncStatus) Subscribe() <-chan *SyncStatusEvent {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	ch := make(chan *SyncStatusEvent, syncEventBufferSize)
	s.eventSubs = append(s.eventSubs, ch)
	return ch
}

// Unsubscribe removes a subscription channel
func (s *SyncStatus) Unsubscribe(ch <-chan *SyncStatusEvent) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	for i, sub := range s.eventSubs {
		if sub == ch {
			close(sub)
			s.eventSubs = append(s.eventSubs[:i], s.eventSubs[i+1:]...)
			break
		}
	}
}

// broadcastEvent sends an event to all subscribers
func (s *SyncStatus) broadcastEvent(path SyncPath, status *PathStatus) {
	s.eventMu.RLock()
	defer s.eventMu.RUnlock()

	event := &SyncStatusEvent{Path: path, Status: status}
	for _, sub := range s.eventSubs {
		select {
		case sub <- event:
		default:
			// Channel is full, skip to avoid blocking
		}
	}
}

// getOrCreateStatus gets existing status or creates a new one
func (s *SyncStatus) getOrCreateStatus(path SyncPath) *PathStatus {
	if status, exists := s.files[path]; exists {
		return status
	}

	status := &PathStatus{
		SyncState:     SyncStatePending,
		ConflictState: ConflictStateNone,
		Progress:      progressMin,
		LastUpdated:   time.Now(),
		ErrorCount:    0,
	}
	s.files[path] = status
	return status
}

// SetSyncing sets a file to syncing state, preserving conflicted/rejected file state
func (s *SyncStatus) SetSyncing(path SyncPath) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.getOrCreateStatus(path)

	// conflicted/rejected states file states are preserved during re-sync operations
	status.SyncState = SyncStateSyncing
	status.Progress = progressMin
	status.Error = nil
	status.LastUpdated = time.Now()

	s.broadcastEvent(path, status)
}

// SetProgress updates the progress of a syncing file
func (s *SyncStatus) SetProgress(path SyncPath, progress float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.getOrCreateStatus(path)
	status.Progress = progress
	status.LastUpdated = time.Now()

	s.broadcastEvent(path, status)
}

// SetCompleted sets a file to completed state, preserving file state and removing only clean files
func (s *SyncStatus) SetCompleted(path SyncPath) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.getOrCreateStatus(path)
	status.SyncState = SyncStateCompleted
	status.Progress = progressMax
	status.Error = nil
	status.LastUpdated = time.Now()

	// Only remove clean files from tracking - keep rejected/conflicted files in tracking
	if status.ConflictState == ConflictStateNone {
		delete(s.files, path)
	}
	s.broadcastEvent(path, status)
}

// SetCompletedAndRemove explicitly clears a file to clean state and removes it from tracking
func (s *SyncStatus) SetCompletedAndRemove(path SyncPath) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.getOrCreateStatus(path)
	status.SyncState = SyncStateCompleted
	status.ConflictState = ConflictStateNone
	status.Progress = progressMax
	status.Error = nil
	status.LastUpdated = time.Now()

	s.broadcastEvent(path, status)
	delete(s.files, path)
}

// SetError sets a file to error state
func (s *SyncStatus) SetError(path SyncPath, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.getOrCreateStatus(path)
	status.SyncState = SyncStateError
	status.Error = err
	status.ErrorCount++
	status.LastUpdated = time.Now()

	// what an awful place to log this
	// log max allowed error count reached, file will be ignored
	if status.ErrorCount >= maxRetryCount {
		slog.Error("sync", "status", "Error", "path", path, "count", status.ErrorCount, "error", "retry limit reached. file will be excluded from syncing")
	}

	s.broadcastEvent(path, status)
}

// SetConflicted marks a file as conflicted
func (s *SyncStatus) SetConflicted(path SyncPath) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.getOrCreateStatus(path)
	status.SyncState = SyncStateCompleted
	status.ConflictState = ConflictStateConflicted
	status.Progress = progressMax
	status.Error = nil
	status.LastUpdated = time.Now()

	s.broadcastEvent(path, status)
}

// SetRejected marks a file as rejected
func (s *SyncStatus) SetRejected(path SyncPath) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.getOrCreateStatus(path)
	status.SyncState = SyncStateCompleted
	status.ConflictState = ConflictStateRejected
	status.Progress = progressMax
	status.Error = nil
	status.LastUpdated = time.Now()

	s.broadcastEvent(path, status)
}

// GetStatus returns the status of a specific file
func (s *SyncStatus) GetStatus(path SyncPath) (*PathStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status, exists := s.files[path]
	return status, exists
}

func (s *SyncStatus) GetErrorCount(path SyncPath) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if status, ok := s.files[path]; ok {
		return status.ErrorCount
	}
	return 0
}

// GetConflictedFiles returns a map of all conflicted files
func (s *SyncStatus) GetConflictedFiles() map[SyncPath]*PathStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conflicted := make(map[SyncPath]*PathStatus)
	for path, status := range s.files {
		if status.ConflictState == ConflictStateConflicted {
			conflicted[path] = status
		}
	}
	return conflicted
}

// GetRejectedFiles returns a map of all rejected files
func (s *SyncStatus) GetRejectedFiles() map[SyncPath]*PathStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rejected := make(map[SyncPath]*PathStatus)
	for path, status := range s.files {
		if status.ConflictState == ConflictStateRejected {
			rejected[path] = status
		}
	}
	return rejected
}

// GetSyncingFileCount returns the number of files currently syncing
func (s *SyncStatus) GetSyncingFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, status := range s.files {
		if status.SyncState == SyncStateSyncing {
			count++
		}
	}
	return count
}

// GetConflictedFileCount returns the number of conflicted files
func (s *SyncStatus) GetConflictedFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, status := range s.files {
		if status.ConflictState == ConflictStateConflicted {
			count++
		}
	}
	return count
}

// GetRejectedFileCount returns the number of rejected files
func (s *SyncStatus) GetRejectedFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, status := range s.files {
		if status.ConflictState == ConflictStateRejected {
			count++
		}
	}
	return count
}

// GetAllStatus returns a copy of all file statuses
func (s *SyncStatus) GetAllStatus() map[SyncPath]*PathStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[SyncPath]*PathStatus, len(s.files))
	for path, status := range s.files {
		// Create a copy to avoid race conditions
		statusCopy := *status
		result[path] = &statusCopy
	}
	return result
}

// Cleanup removes completed files older than the specified duration
func (s *SyncStatus) Cleanup(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for path, status := range s.files {
		if status.SyncState == SyncStateCompleted &&
			status.ConflictState == ConflictStateNone &&
			status.LastUpdated.Before(cutoff) {
			delete(s.files, path)
		}
	}
}

func (s *SyncStatus) Close() {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	for _, sub := range s.eventSubs {
		close(sub)
	}

	s.eventSubs = make([]chan *SyncStatusEvent, 0)
	s.files = make(map[SyncPath]*PathStatus)
}
