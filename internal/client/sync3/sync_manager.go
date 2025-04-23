package sync3

import (
	"context"
	"fmt"

	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftsdk"
)

type SyncManager struct {
	sdk       *syftsdk.SyftSDK
	workspace *workspace.Workspace
	engine    *SyncEngine
	watcher   *FileWatcher
	ignore    *SyncIgnore
	priority  *SyncPriority
}

func NewManager(workspace *workspace.Workspace, sdk *syftsdk.SyftSDK) (*SyncManager, error) {
	watcher := NewFileWatcher(workspace.DatasitesDir)
	ignore := NewSyncIgnore(workspace.DatasitesDir)
	priority := NewSyncPriority(workspace.DatasitesDir)
	engine := NewSyncEngine(workspace, sdk, ignore, priority, watcher)

	return &SyncManager{
		sdk:       sdk,
		workspace: workspace,
		watcher:   watcher,
		ignore:    ignore,
		priority:  priority,
		engine:    engine,
	}, nil
}

func (m *SyncManager) Start(ctx context.Context) error {
	if err := m.watcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	if err := m.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}
	return nil
}

func (m *SyncManager) Stop() error {
	m.watcher.Stop()
	return m.engine.Stop()
}
