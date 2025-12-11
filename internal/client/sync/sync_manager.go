package sync

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftsdk"
)

type SyncManager struct {
	sdk       *syftsdk.SyftSDK
	workspace *workspace.Workspace
	engine    *SyncEngine
	ignore    *SyncIgnoreList
	priority  *SyncPriorityList
}

func NewManager(workspace *workspace.Workspace, sdk *syftsdk.SyftSDK) (*SyncManager, error) {
	ignoreList := NewSyncIgnoreList(workspace.DatasitesDir)
	priorityList := NewSyncPriorityList(workspace.DatasitesDir)
	engine, err := NewSyncEngine(workspace, sdk, ignoreList, priorityList)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync engine: %w", err)
	}

	return &SyncManager{
		sdk:       sdk,
		workspace: workspace,
		ignore:    ignoreList,
		priority:  priorityList,
		engine:    engine,
	}, nil
}

func (m *SyncManager) Start(ctx context.Context) error {
	slog.Info("sync manager start")

	// load the ignore list
	m.ignore.Load()

	if err := m.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}
	return nil
}

func (m *SyncManager) Stop() error {
	slog.Info("sync manager stop")
	return m.engine.Stop()
}

func (m *SyncManager) GetSyncStatus() *SyncStatus {
	return m.engine.syncStatus
}

func (m *SyncManager) GetUploadRegistry() *UploadRegistry {
	return m.engine.uploadRegistry
}

func (m *SyncManager) TriggerSync() {
	go func() {
		if err := m.engine.RunSync(context.Background()); err != nil {
			slog.Error("triggered sync", "error", err)
		}
	}()
}
