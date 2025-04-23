package client

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/datasite"
)

type Client struct {
	ds *datasite.Datasite
}

func New(config *config.Config) (*Client, error) {
	ds, err := datasite.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create datasite: %w", err)
	}

	return &Client{
		ds: ds,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	if err := c.ds.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()
	slog.Info("received interrupt signal, stopping client")
	c.ds.Stop()
	return nil
}
