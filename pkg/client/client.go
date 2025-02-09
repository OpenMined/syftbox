package client

import (
	"context"
	"log/slog"
	"sync"

	"github.com/yashgorana/syftbox-go/pkg/config"
	"github.com/yashgorana/syftbox-go/pkg/fswatch"
)

type Client struct {
	config    *config.Config
	workspace *Workspace
	watcher   *fswatch.Watcher

	wg sync.WaitGroup
}

func Default() (*Client, error) {
	watcher, err := fswatch.New()
	if err != nil {
		return nil, err
	}

	config := config.Default()
	return &Client{
		config:    config,
		workspace: NewWorkspace(config.DataDir),
		watcher:   watcher,
	}, nil
}

func (c *Client) Run(ctx context.Context) error {
	slog.Info("Starting client", "data", c.workspace.Root, "config", c.config.Path)
	err := c.workspace.CreateDirs()
	if err != nil {
		return err
	}

	err = c.watcher.Add(c.workspace.DatasitesDir)
	if err != nil {
		return err
	}

	// Start file watcher
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := c.watcher.Start(ctx); err != nil {
			slog.Error("watcher error", "error", err)
		}
	}()

	// Process events
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.processEvents(ctx)
	}()

	// Handle shutdown
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		<-ctx.Done()
		slog.Info("received shutdown signal")
		if err := c.Stop(ctx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	c.wg.Wait()
	slog.Info("client shutdown completed")
	return nil
}

func (c *Client) Stop(ctx context.Context) error {
	return c.watcher.Stop(ctx)
}

func (c *Client) processEvents(ctx context.Context) {
	for {
		select {
		case event, ok := <-c.watcher.Events:
			if !ok {
				return
			}
			slog.Info("fs event", "event", event.Op.String(), "path", event.Name)
		case err, ok := <-c.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("fs error", "error", err)
		case <-ctx.Done():
			return
		}
	}
}
