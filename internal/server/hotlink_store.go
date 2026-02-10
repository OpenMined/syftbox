package server

import (
	"sync"
	"time"
)

type hotlinkSession struct {
	ID       string
	Path     string
	FromUser string
	ToUser   string
	FromConn string
	Created  time.Time
	Accepted map[string]string // connID -> user
}

type hotlinkStore struct {
	mu       sync.RWMutex
	sessions map[string]*hotlinkSession
}

func newHotlinkStore() *hotlinkStore {
	return &hotlinkStore{
		sessions: make(map[string]*hotlinkSession),
	}
}

func (s *hotlinkStore) Open(id, path, fromUser, toUser, fromConn string) *hotlinkSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := &hotlinkSession{
		ID:       id,
		Path:     path,
		FromUser: fromUser,
		ToUser:   toUser,
		FromConn: fromConn,
		Created:  time.Now(),
		Accepted: make(map[string]string),
	}
	s.sessions[id] = session
	return session
}

func (s *hotlinkStore) Get(id string) (*hotlinkSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *hotlinkStore) Accept(id, connID, user string) (*hotlinkSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	session.Accepted[connID] = user
	return session, true
}

func (s *hotlinkStore) RemoveAccepted(id, connID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return
	}
	delete(session.Accepted, connID)
}

func (s *hotlinkStore) Close(id string) (*hotlinkSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	delete(s.sessions, id)
	return session, true
}
