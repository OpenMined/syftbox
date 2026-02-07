package sync

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

type StagedACL struct {
	Path    string
	Content []byte
	ETag    string
}

type PendingACLSet struct {
	Manifest *syftmsg.ACLManifest
	Received map[string]*StagedACL // path -> staged ACL
	Applied  bool
	Created  time.Time
}

func (p *PendingACLSet) IsComplete() bool {
	for _, entry := range p.Manifest.ACLOrder {
		if _, ok := p.Received[entry.Path]; !ok {
			return false
		}
	}
	return true
}

func (p *PendingACLSet) ReceivedCount() int {
	return len(p.Received)
}

func (p *PendingACLSet) ExpectedCount() int {
	return len(p.Manifest.ACLOrder)
}

type ACLStagingManager struct {
	mu       sync.RWMutex
	pending  map[string]*PendingACLSet // datasite -> pending ACL set
	onReady  func(datasite string, acls []*StagedACL)
	recent   map[string]time.Time // datasite -> last ACL activity
	ttl      time.Duration
	grace    time.Duration
	now      func() time.Time
}

type ACLStagingOption func(*ACLStagingManager)

func WithACLStagingTTL(ttl time.Duration) ACLStagingOption {
	return func(m *ACLStagingManager) {
		m.ttl = ttl
	}
}

func WithACLStagingGrace(grace time.Duration) ACLStagingOption {
	return func(m *ACLStagingManager) {
		m.grace = grace
	}
}

func WithACLStagingNow(now func() time.Time) ACLStagingOption {
	return func(m *ACLStagingManager) {
		m.now = now
	}
}

func NewACLStagingManager(onReady func(datasite string, acls []*StagedACL), opts ...ACLStagingOption) *ACLStagingManager {
	grace := 10 * time.Minute
	if v := os.Getenv("SYFTBOX_ACL_STAGING_GRACE_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			grace = time.Duration(ms) * time.Millisecond
		}
	}
	m := &ACLStagingManager{
		pending: make(map[string]*PendingACLSet),
		onReady: onReady,
		recent:  make(map[string]time.Time),
		ttl:     30 * time.Second,
		grace:   grace,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *ACLStagingManager) SetManifest(manifest *syftmsg.ACLManifest) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.pruneLocked(now)

	existing := m.pending[manifest.Datasite]
	if existing != nil && !existing.Applied {
		slog.Info("acl staging replacing pending manifest", "datasite", manifest.Datasite, "oldCount", existing.ExpectedCount(), "newCount", len(manifest.ACLOrder))
	}

	m.pending[manifest.Datasite] = &PendingACLSet{
		Manifest: manifest,
		Received: make(map[string]*StagedACL),
		Applied:  false,
		Created:  now,
	}
	m.recordActivityLocked(manifest.Datasite, now)

	slog.Info("acl staging manifest set", "datasite", manifest.Datasite, "expectedCount", len(manifest.ACLOrder))
}

func (m *ACLStagingManager) StageACL(datasite, path string, content []byte, etag string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.pruneLocked(now)
	m.recordActivityLocked(datasite, now)

	pending := m.pending[datasite]
	if pending == nil || pending.Applied {
		return false
	}

	isExpected := false
	for _, entry := range pending.Manifest.ACLOrder {
		if entry.Path == path {
			isExpected = true
			break
		}
	}

	if !isExpected {
		slog.Debug("acl staging unexpected path", "datasite", datasite, "path", path)
		return false
	}

	pending.Received[path] = &StagedACL{
		Path:    path,
		Content: content,
		ETag:    etag,
	}

	slog.Info("acl staging received", "datasite", datasite, "path", path, "received", pending.ReceivedCount(), "expected", pending.ExpectedCount())

	if pending.IsComplete() {
		slog.Info("acl staging complete", "datasite", datasite, "count", pending.ExpectedCount())
		pending.Applied = true

		orderedACLs := make([]*StagedACL, 0, len(pending.Manifest.ACLOrder))
		for _, entry := range pending.Manifest.ACLOrder {
			if acl, ok := pending.Received[entry.Path]; ok {
				orderedACLs = append(orderedACLs, acl)
			}
		}

		if m.onReady != nil {
			go m.onReady(datasite, orderedACLs)
		}
	}

	return true
}

func (m *ACLStagingManager) HasPendingManifest(datasite string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pruneLocked(m.now())

	pending := m.pending[datasite]
	return pending != nil && !pending.Applied
}

func (m *ACLStagingManager) GetPendingPaths(datasite string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pruneLocked(m.now())

	pending := m.pending[datasite]
	if pending == nil {
		return nil
	}

	paths := make([]string, 0, len(pending.Manifest.ACLOrder))
	for _, entry := range pending.Manifest.ACLOrder {
		paths = append(paths, entry.Path)
	}
	return paths
}

func (m *ACLStagingManager) IsPendingACLPath(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.pruneLocked(now)

	normalizedPath := strings.ReplaceAll(path, "\\", "/")
	datasite := pathDatasite(normalizedPath)
	if datasite == "" {
		return false
	}

	if isACLFilePath(normalizedPath) {
		if last, ok := m.recent[datasite]; ok && now.Sub(last) <= m.grace {
			return true
		}
	}

	for _, pending := range m.pending {
		if pending == nil || pending.Applied {
			continue
		}
		for _, entry := range pending.Manifest.ACLOrder {
			normalizedEntry := strings.ReplaceAll(entry.Path, "\\", "/")
			aclFilePath := normalizedEntry + "/syft.pub.yaml"
			if normalizedPath == aclFilePath || normalizedPath == normalizedEntry {
				return true
			}
		}
	}
	return false
}

func (m *ACLStagingManager) NoteACLActivity(datasite string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.pruneLocked(now)
	m.recordActivityLocked(datasite, now)
}

func (m *ACLStagingManager) recordActivityLocked(datasite string, now time.Time) {
	if datasite == "" {
		return
	}
	m.recent[datasite] = now
}

func (m *ACLStagingManager) pruneLocked(now time.Time) {
	for datasite, pending := range m.pending {
		if pending == nil {
			delete(m.pending, datasite)
			continue
		}
		if pending.Applied || now.Sub(pending.Created) > m.ttl {
			delete(m.pending, datasite)
		}
	}

	for datasite, last := range m.recent {
		if now.Sub(last) > m.grace {
			delete(m.recent, datasite)
		}
	}
}

func pathDatasite(path string) string {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func isACLFilePath(path string) bool {
	return path == "syft.pub.yaml" || strings.HasSuffix(path, "/syft.pub.yaml")
}
