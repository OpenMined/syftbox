package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/openmined/syftbox/internal/utils"
)

var (
	home, _           = os.UserHomeDir()
	DefaultConfigPath = filepath.Join(home, ".syftbox", "config.json")
	DefaultServerURL  = "https://syftboxdev.openmined.org"
	DefaultClientURL  = "http://localhost:8080"
)

type Config struct {
	DataDir     string `json:"data_dir"`
	Email       string `json:"email"`
	ServerURL   string `json:"server_url"`
	ClientURL   string `json:"client_url"`
	AppsEnabled bool   `json:"-"`
	Path        string `json:"-"`
}

func (c *Config) Save(path string) error {
	if err := utils.EnsureParent(path); err != nil {
		return err
	}

	c.ClientURL = DefaultClientURL

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func LoadClientConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	cfg.Path = path
	cfg.AppsEnabled = true
	cfg.ServerURL = DefaultServerURL

	return &cfg, nil
}
