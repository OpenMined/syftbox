package client

import (
	"fmt"
	"os"
)

const (
	EnvConfigPath     = "SYFTBOX_CLIENT_CONFIG_PATH"
	DefaultServerURL  = "https://syftbox.yashg.dev"
	DefaultClientPort = 38080
)

var (
	Home, _             = os.UserHomeDir()
	DefaultConfigPath   = ".data/config.json"
	DefaultWorkspaceDir = ".data"
	ErrInvalidDir       = fmt.Errorf("invalid directory")
)

func init() {
	if env_conf_path := os.Getenv(EnvConfigPath); env_conf_path != "" {
		DefaultConfigPath = env_conf_path
	}
}

type ClientConfig struct {
	DataDir   string `json:"data_dir"`
	Email     string `json:"email"`
	ServerURL string `json:"server_url"`
	Path      string `json:"-"`
}

// Create a new configuration with the given data directory and server URL
func NewClientConfig(dataDir string, server string) *ClientConfig {
	return &ClientConfig{
		DataDir:   dataDir,
		ServerURL: server,
		Path:      DefaultConfigPath,
	}
}

// Return the default configuration
func DefaultClientConfig() *ClientConfig {
	return NewClientConfig(DefaultWorkspaceDir, DefaultServerURL)
}
