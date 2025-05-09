package sync

import (
	"log/slog"
	"sync"
)

type PathStatus struct{}
type SyncStatusEvent struct{}

type SyncStatus struct {
	status map[string]PathStatus
	mu     sync.RWMutex
}

func NewSyncStatus() *SyncStatus {
	return &SyncStatus{
		status: make(map[string]PathStatus),
		mu:     sync.RWMutex{},
	}
}

func (s *SyncStatus) SetSyncing(path string, src string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[path] = PathStatus{}
	slog.Debug("sync status", "path", path, "status", "SYNCING", "src", src)
}

func (s *SyncStatus) SetProgress(path string, progress float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[path] = PathStatus{}
	slog.Debug("sync status", "path", path, "status", "PROGRESS", "progress", progress)
}

func (s *SyncStatus) SetError(path string, src string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[path] = PathStatus{}
	slog.Debug("sync status", "path", path, "status", "ERROR", "src", src)
}

func (s *SyncStatus) SetCompleted(path string, src string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.status, path)
	slog.Debug("sync status", "path", path, "status", "COMPLETED", "src", src)
}

func (s *SyncStatus) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.status)
}

func (s *SyncStatus) IsSyncing(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.status[path]
	return ok
}
