package sync

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openmined/syftbox/internal/client/messaging"
	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftsdk"
)

type SyncManager struct {
	sdk       *syftsdk.SyftSDK
	workspace *workspace.Workspace
	engine    *SyncEngine
	watcher   *FileWatcher
	ignore    *SyncIgnoreList
	priority  *SyncPriorityList
}

func NewManager(workspace *workspace.Workspace, sdk *syftsdk.SyftSDK, httpMsgMgr *messaging.HttpMsgManager) (*SyncManager, error) {
	watcher := NewFileWatcher(workspace.DatasitesDir)
	ignore := NewSyncIgnoreList(workspace.DatasitesDir)
	priority := NewSyncPriorityList(workspace.DatasitesDir)
	engine, err := NewSyncEngine(workspace, sdk, ignore, priority, watcher, httpMsgMgr)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync engine: %w", err)
	}

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
	slog.Info("sync manager start")
	if err := m.watcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	if err := m.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}
	return nil
}

func (m *SyncManager) Stop() error {
	slog.Info("sync manager stop")
	m.watcher.Stop()
	return m.engine.Stop()
}
