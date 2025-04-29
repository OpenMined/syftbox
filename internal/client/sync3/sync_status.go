package sync3

import "sync"

type SyncStatus struct {
	syncStatus map[string]struct{}
	mu         sync.RWMutex
}

func NewSyncStatus() *SyncStatus {
	return &SyncStatus{
		syncStatus: make(map[string]struct{}),
		mu:         sync.RWMutex{},
	}
}

func (s *SyncStatus) SetSyncing(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncStatus[path] = struct{}{}
}

func (s *SyncStatus) UnsetSyncing(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.syncStatus, path)
}

func (s *SyncStatus) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.syncStatus)
}

func (s *SyncStatus) IsSyncing(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.syncStatus[path]
	return ok
}
