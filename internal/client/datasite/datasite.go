package datasite

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/sync"
	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftsdk"
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
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	ws, err := workspace.NewWorkspace(config.DataDir, config.Email)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}

	sdk, err := syftsdk.New(config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("sdk: %w", err)
	}

	appSched := apps.NewScheduler(ws.AppsDir, config.Path)
	appMgr := apps.NewManager(ws.AppsDir)

	sync, err := sync.NewManager(ws, sdk)
	if err != nil {
		return nil, fmt.Errorf("sync manager: %w", err)
	}

	return &Datasite{
		config:       config,
		sdk:          sdk,
		workspace:    ws,
		appScheduler: appSched,
		appManager:   appMgr,
		sync:         sync,
	}, nil
}

func (d *Datasite) Start(ctx context.Context) error {
	slog.Info("syftbox client start", "datadir", d.config.DataDir, "email", d.config.Email, "serverURL", d.config.ServerURL, "clientURL", d.config.ClientURL)

	// Setup local datasite first.
	if err := d.workspace.Setup(); err != nil {
		return fmt.Errorf("setup datasite: %w", err)
	}

	// persist the config
	if err := d.config.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// placeholder to "Login" to the server
	if err := d.sdk.Login(d.config.Email); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// Start app scheduler
	if d.config.AppsEnabled {
		if err := d.appScheduler.Start(ctx); err != nil {
			slog.Error("app scheduler", "error", err)
		}
	} else {
		slog.Info("apps disabled")
	}

	// Start sync manager. this will block for the first sync cycle.
	if err := d.sync.Start(ctx); err != nil {
		return fmt.Errorf("sync manager: %w", err)
	}

	return nil
}

func (d *Datasite) Stop() {
	d.sync.Stop()
	d.sdk.Close()
	d.workspace.Unlock()
	slog.Info("syftbox client stop")
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
