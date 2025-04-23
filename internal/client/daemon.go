package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"golang.org/x/sync/errgroup"
)

type ClientDaemon struct {
	mgr *datasitemgr.DatasiteManger
	cps *ControlPlaneServer
}

func NewClientDaemon(config *ControlPlaneConfig) (*ClientDaemon, error) {
	mgr := datasitemgr.NewDatasiteManger()
	cps, err := NewControlPlaneServer(config, mgr)
	if err != nil {
		return nil, err
	}
	return &ClientDaemon{
		mgr: mgr,
		cps: cps,
	}, nil
}

func (c *ClientDaemon) Start(ctx context.Context) error {
	slog.Info("client daemon start")

	// Create errgroup with derived context
	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		if err := c.mgr.Start(ctx); err != nil {
			return fmt.Errorf("failed to start datasite manager: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		if err := c.cps.Start(ctx); err != nil {
			return fmt.Errorf("failed to start control plane: %w", err)
		}
		return nil
	})

	// Launch goroutine to handle shutdown on context cancellation
	eg.Go(func() error {
		<-egCtx.Done()
		slog.Info("received interrupt signal, stopping daemon")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return c.Stop(shutdownCtx)
	})

	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("client daemon failure", "error", err)
		return err
	}

	slog.Info("client daemon stopped")
	return nil
}

func (c *ClientDaemon) Stop(ctx context.Context) error {
	c.mgr.Stop()
	if err := c.cps.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop control plane: %w", err)
	}
	return nil
}
