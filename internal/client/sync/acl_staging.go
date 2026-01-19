package sync

import (
	"log/slog"
	"strings"
	"sync"

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
}

func NewACLStagingManager(onReady func(datasite string, acls []*StagedACL)) *ACLStagingManager {
	return &ACLStagingManager{
		pending: make(map[string]*PendingACLSet),
		onReady: onReady,
	}
}

func (m *ACLStagingManager) SetManifest(manifest *syftmsg.ACLManifest) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.pending[manifest.Datasite]
	if existing != nil && !existing.Applied {
		slog.Info("acl staging replacing pending manifest", "datasite", manifest.Datasite, "oldCount", existing.ExpectedCount(), "newCount", len(manifest.ACLOrder))
	}

	m.pending[manifest.Datasite] = &PendingACLSet{
		Manifest: manifest,
		Received: make(map[string]*StagedACL),
		Applied:  false,
	}

	slog.Info("acl staging manifest set", "datasite", manifest.Datasite, "expectedCount", len(manifest.ACLOrder))
}

func (m *ACLStagingManager) StageACL(datasite, path string, content []byte, etag string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

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
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := m.pending[datasite]
	return pending != nil && !pending.Applied
}

func (m *ACLStagingManager) GetPendingPaths(datasite string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

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
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedPath := strings.ReplaceAll(path, "\\", "/")
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
