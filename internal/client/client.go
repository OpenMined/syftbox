package client

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yashgorana/syftbox-go/internal/client/apps"
	"github.com/yashgorana/syftbox-go/internal/client/datasite"
	"github.com/yashgorana/syftbox-go/internal/client/sync"
	"github.com/yashgorana/syftbox-go/internal/localhttp"
	"github.com/yashgorana/syftbox-go/internal/syftsdk"
)

type Client struct {
	config       *Config
	datasite     *datasite.LocalDatasite
	sdk          *syftsdk.SyftSDK
	sync         *sync.SyncManager
	appScheduler *apps.AppScheduler
	uiServer     *localhttp.Server
}

func New(config *Config) (*Client, error) {
	ds, err := datasite.NewLocalDatasite(config.DataDir, config.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to create datasite: %w", err)
	}

	sdk, err := syftsdk.New(config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create sdk: %w", err)
	}

	appSched := apps.NewScheduler(ds.AppsDir, config.Path)

	sync := sync.NewManager(sdk, ds)

	// Create UI bridge server if enabled
	var uiServer *localhttp.Server
	if config.UIBridge.Enabled {
		uiServer, err = localhttp.New(config.UIBridge)
		if err != nil {
			return nil, fmt.Errorf("failed to create UI bridge server: %w", err)
		}
	}

	return &Client{
		config:       config,
		datasite:     ds,
		sdk:          sdk,
		sync:         sync,
		appScheduler: appSched,
		uiServer:     uiServer,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	slog.Info("syftgo client start", "datadir", c.config.DataDir, "email", c.config.Email, "server", c.config.ServerURL)
	// Setup local datasite first
	if err := c.datasite.Bootstrap(); err != nil {
		return fmt.Errorf("failed to bootstrap datasite: %w", err)
	}

	if err := c.sdk.Login(c.config.Email); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	// Start UI bridge server if enabled
	if c.uiServer != nil {
		go func() {
			if err := c.uiServer.Start(ctx); err != nil {
				slog.Error("UI bridge server error", "error", err)
			}
		}()
	}

	// Start sync manager
	if err := c.sync.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sync manager: %w", err)
	}

	// Start app scheduler
	if c.config.AppsEnabled {
		if err := c.appScheduler.Start(ctx); err != nil {
			slog.Error("failed to start app scheduler", "error", err)
		}
	} else {
		slog.Info("apps disabled")
	}

	<-ctx.Done()
	slog.Info("received interrupt signal, stopping client")

	// Stop UI bridge server
	if c.uiServer != nil {
		if err := c.uiServer.Stop(); err != nil {
			slog.Error("Error stopping UI bridge server", "error", err)
		}
	}

	c.sync.Stop()
	c.sdk.Close()
	slog.Info("syftgo client stop")
	return nil
}
