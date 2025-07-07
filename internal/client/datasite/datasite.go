package datasite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/sync"
	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
)

type Datasite struct {
	id           string
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

	sdk, err := syftsdk.New(&syftsdk.SyftSDKConfig{
		BaseURL:      config.ServerURL,
		Email:        config.Email,
		RefreshToken: config.RefreshToken,
		AccessToken:  config.AccessToken,
	})
	if err != nil {
		return nil, fmt.Errorf("sdk: %w", err)
	}

	appMgr := apps.NewManager(ws.AppsDir, ws.MetadataDir)
	appSched := apps.NewAppScheduler(appMgr, config.Path)

	sync, err := sync.NewManager(ws, sdk)
	if err != nil {
		return nil, fmt.Errorf("sync manager: %w", err)
	}

	return &Datasite{
		id:           utils.TokenHex(3),
		config:       config,
		sdk:          sdk,
		workspace:    ws,
		appScheduler: appSched,
		appManager:   appMgr,
		sync:         sync,
	}, nil
}

func (d *Datasite) Start(ctx context.Context) error {
	slog.Info("datasite start", "id", d.id, "config", d.config)

	// Setup local datasite first.
	if err := d.workspace.Setup(); err != nil {
		return fmt.Errorf("setup datasite: %w", err)
	}

	// persist the config
	if err := d.config.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// authenticate with the server
	if err := d.handleClientAuth(ctx); err != nil {
		return fmt.Errorf("handle client auth: %w", err)
	}

	// Start app scheduler
	if err := d.appScheduler.Start(ctx); err != nil {
		slog.Error("app scheduler", "error", err)
	}

	// Start sync manager. this will block for the first sync cycle.
	if err := d.sync.Start(ctx); err != nil {
		return fmt.Errorf("sync manager: %w", err)
	}

	return nil
}

func (d *Datasite) Stop() {
	d.appScheduler.Stop()
	d.sync.Stop()
	d.sdk.Close()
	if err := d.workspace.Unlock(); err != nil {
		slog.Error("datasite stop", "error", err)
	}
	slog.Info("datasite stopped", "id", d.id)
}

func (d *Datasite) GetSDK() *syftsdk.SyftSDK {
	return d.sdk
}

func (d *Datasite) GetConfig() *config.Config {
	return d.config
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

func (d *Datasite) handleClientAuth(ctx context.Context) error {
	// setup callback to update the refresh token in the config
	d.sdk.OnAuthTokenUpdate(d.updateRefreshToken)

	slog.Info("authenticating with the server")
	if err := d.sdk.Authenticate(ctx); err != nil {
		if errors.Is(err, syftsdk.ErrNoRefreshToken) {
			return fmt.Errorf("no refresh token found, please login again")
		} else {
			return fmt.Errorf("authenticate: %w", err)
		}
	}
	slog.Info("authenticated", "user", d.config.Email)

	return nil
}

func (d *Datasite) updateRefreshToken(refreshToken string) {
	// just in case we decide to not rotate refresh tokens
	if refreshToken == "" {
		return
	}

	d.config.RefreshToken = refreshToken
	if err := d.config.Save(); err != nil {
		slog.Error("save config", "error", err)
	}
}
