package client

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yashgorana/syftbox-go/internal/client/apps"
	"github.com/yashgorana/syftbox-go/internal/client/datasite"
	"github.com/yashgorana/syftbox-go/internal/client/syftapi"
	"github.com/yashgorana/syftbox-go/internal/client/sync"
)

type Client struct {
	config       *Config
	datasite     *datasite.LocalDatasite
	api          *syftapi.SyftAPI
	sync         *sync.SyncManager
	appScheduler *apps.AppScheduler
}

func New(config *Config) (*Client, error) {
	ds, err := datasite.NewLocalDatasite(config.DataDir, config.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to create datasite: %w", err)
	}

	api, err := syftapi.New(config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create api: %w", err)
	}

	appSched := apps.NewScheduler(ds.AppsDir)

	sync := sync.NewManager(api, ds)

	return &Client{
		config:       config,
		datasite:     ds,
		api:          api,
		sync:         sync,
		appScheduler: appSched,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	slog.Info("syftgo client start", "datadir", c.config.DataDir, "email", c.config.Email, "server", c.config.ServerURL)
	// Setup local datasite first
	if err := c.datasite.Bootstrap(); err != nil {
		return fmt.Errorf("failed to bootstrap datasite: %w", err)
	}

	if err := c.api.Login(c.config.Email); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	// Start sync manager
	if err := c.sync.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sync manager: %w", err)
	}

	// Start app scheduler
	if err := c.appScheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start app scheduler: %w", err)
	}

	<-ctx.Done()
	slog.Info("recieved interrupt signal, stopping client")
	c.sync.Stop()
	c.api.Close()
	slog.Info("syftgo client stop")
	return nil
}
