package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	EnvConfigPath     = "SYFTBOX_CLIENT_CONFIG_PATH"
	DefaultServerURL  = "https://syftbox.yashg.dev"
	DefaultClientPort = 38080
)

var (
	Home, _             = os.UserHomeDir()
	DefaultConfigPath   = filepath.Join(Home, ".syftgo.json")
	DefaultWorkspaceDir = filepath.Join(Home, "syftgo-data")
	ErrInvalidDir       = fmt.Errorf("invalid directory")
)

func init() {
	if env_conf_path := os.Getenv(EnvConfigPath); env_conf_path != "" {
		DefaultConfigPath = env_conf_path
	}
}

type Config struct {
	DataDir   string `json:"data_dir"`
	Email     string `json:"email"`
	ServerURL string `json:"server_url"`
	Path      string `json:"-"`
}

// Create a new configuration with the given data directory and server URL
func New(dataDir string, server string) *Config {
	return &Config{
		DataDir:   dataDir,
		ServerURL: server,
		Path:      DefaultConfigPath,
	}
}

// Return the default configuration
func Default() *Config {
	return New(DefaultWorkspaceDir, DefaultServerURL)
}
