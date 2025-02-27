package client

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/radovskyb/watcher"
	"github.com/rjeczalik/notify"
)

type Client struct {
	config   *Config
	datasite *LocalDatasite
}

func New(config *Config) (*Client, error) {
	ds, err := NewLocalDatasite(config.DataDir, config.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to create datasite: %w", err)
	}
	return &Client{
		config:   config,
		datasite: ds,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	slog.Info("syftgo client start", "datadir", c.config.DataDir, "email", c.config.Email, "server", c.config.ServerURL)
	// Setup local datasite first
	if err := c.datasite.Bootstrap(); err != nil {
		return fmt.Errorf("failed to bootstrap datasite: %w", err)
	}

	if err := c.startFileWatcher(ctx); err != nil {
		return fmt.Errorf("fs notify error: %w", err)
	}

	if err := c.startPollWatcher(ctx); err != nil {
		return fmt.Errorf("fs poll error: %w", err)
	}

	<-ctx.Done()
	slog.Info("syftgo client stop")
	return nil
}

func (c *Client) startFileWatcher(ctx context.Context) error {
	recursivePath := c.datasite.DatasitesDir + "/..."
	chanEvents := make(chan notify.EventInfo, 16)
	slog.Info("fs notify start", "dir", c.datasite.DatasitesDir)
	if err := notify.Watch(recursivePath, chanEvents, notify.Create, notify.Write); err != nil {
		return fmt.Errorf("fs notify error: %w", err)
	}

	// Event deduplication map with mutex
	var (
		mu              sync.RWMutex
		lastEvent       = make(map[string]string)
		cleanupInterval = 100 * time.Millisecond
	)

	// instead of stalling the main event loop
	// lets events in the last 50ms and ignore in the main loop
	// this is a simple way to avoid duplicate events
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				mu.Lock()
				clear(lastEvent)
				mu.Unlock()

			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case event, ok := <-chanEvents:
				if !ok {
					return
				}

				mu.RLock()
				_, ok = lastEvent[event.Path()]
				mu.RUnlock()
				if ok {
					continue
				}

				mu.Lock()
				lastEvent[event.Path()] = event.Path()
				mu.Unlock()

				slog.Info("fs notify event", "event", event)

			case <-ctx.Done():
				slog.Info("fs notify stop")
				notify.Stop(chanEvents)
				return
			}
		}
	}()

	return nil
}

func (c *Client) startPollWatcher(ctx context.Context) error {
	w := watcher.New()
	w.FilterOps(watcher.Move, watcher.Rename, watcher.Remove)

	// Watch this folder for changes.
	if err := w.AddRecursive(c.datasite.DatasitesDir); err != nil {
		return fmt.Errorf("fs poll add error: %w", err)
	}

	go func() {
		defer slog.Info("fs poll event loop stop")
		for {
			select {
			case event, ok := <-w.Event:
				if !ok {
					return
				} else if event.IsDir() {
					continue
				}
				slog.Info("fs poll event", "event", event.Op, "path", event.Path)

			case err := <-w.Error:
				slog.Error("fs poll error", "error", err)

			case <-w.Closed:
				return

			case <-ctx.Done():
				w.Close()
				return
			}
		}
	}()

	go func() {
		slog.Info("fs poll start", "dir", c.datasite.DatasitesDir)

		//! Todo - On SIGINT this function is stuck for duration of the poll interval
		//! hence keeping this in a goroutine for fast exit, but it may leak
		if err := w.Start(time.Millisecond * 2000); err != nil {
			slog.Error("fs poll start error", "error", err)
		}
		slog.Info("fs poll stop")
	}()

	return nil
}
