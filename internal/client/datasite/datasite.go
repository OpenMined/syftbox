package datasite

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yashgorana/syftbox-go/internal/client/apps"
	"github.com/yashgorana/syftbox-go/internal/client/config"
	"github.com/yashgorana/syftbox-go/internal/client/sync"
	"github.com/yashgorana/syftbox-go/internal/client/workspace"
	"github.com/yashgorana/syftbox-go/internal/syftsdk"
)

type Datasite struct {
	config       *config.Config
	sdk          *syftsdk.SyftSDK
	workspace    *workspace.Workspace
	appScheduler *apps.AppScheduler
	appManager   *apps.AppManager
	sync         *sync.SyncManager
}

func New(config *config.Config) (*Datasite, error) {
	ds, err := workspace.NewWorkspace(config.DataDir, config.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to create datasite: %w", err)
	}

	sdk, err := syftsdk.New(config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create sdk: %w", err)
	}

	appSched := apps.NewScheduler(ds.AppsDir, config.Path)
	appMgr := apps.NewManager(ds.AppsDir)

	sync := sync.NewManager(sdk, ds)

	return &Datasite{
		config:       config,
		sdk:          sdk,
		workspace:    ds,
		appScheduler: appSched,
		appManager:   appMgr,
		sync:         sync,
	}, nil
}

func (d *Datasite) Start(ctx context.Context) error {
	slog.Info("syftgo client start", "datadir", d.config.DataDir, "email", d.config.Email, "server", d.config.ServerURL)
	// Setup local datasite first
	if err := d.workspace.Bootstrap(); err != nil {
		return fmt.Errorf("failed to bootstrap datasite: %w", err)
	}
	if err := d.sdk.Login(d.config.Email); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	// Start sync manager
	if err := d.sync.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sync manager: %w", err)
	}

	// Start app scheduler
	if d.config.AppsEnabled {
		if err := d.appScheduler.Start(ctx); err != nil {
			slog.Error("failed to start app scheduler", "error", err)
		}
	} else {
		slog.Info("apps disabled")
	}

	return nil
}

func (d *Datasite) Stop() {
	d.sync.Stop()
	d.sdk.Close()
}

func (d *Datasite) GetSDK() *syftsdk.SyftSDK {
	return d.sdk
}

func (d *Datasite) GetWorkspace() *workspace.Workspace {
	return d.workspace
}

func (d *Datasite) GetAppScheduler() *apps.AppScheduler {
	return d.appScheduler
}

func (d *Datasite) GetAppManager() *apps.AppManager {
	return d.appManager
}

func (d *Datasite) GetSyncManager() *sync.SyncManager {
	return d.sync
}
