package client

import (
	"log/slog"

	"github.com/yashgorana/syftbox-go/pkg/config"
)

type Client struct {
	config    *config.Config
	workspace *Workspace
}

func Default() *Client {
	config := config.Default()
	return &Client{
		config:    config,
		workspace: NewWorkspace(config.DataDir),
	}
}

func (c *Client) Run() error {
	slog.Info("Starting client", "data", c.workspace.Root, "config", c.config.Path)
	err := c.workspace.CreateDirs()
	if err != nil {
		return err
	}
	return nil
}
