package config

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"

	"github.com/openmined/syftbox/internal/utils"
)

var (
	home, _            = os.UserHomeDir()
	DefaultConfigPath  = filepath.Join(home, ".syftbox", "config.json")
	DefaultServerURL   = "https://syftboxdev.openmined.org"
	DefaultClientURL   = "http://localhost:8080"
	DefaultLogFilePath = filepath.Join(home, ".syftbox", "logs", "SyftBoxDaemon.log")
)

type Config struct {
	DataDir      string `json:"data_dir"`
	Email        string `json:"email"`
	ServerURL    string `json:"server_url"`
	ClientURL    string `json:"client_url,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AccessToken  string `json:"-"`
	AppsEnabled  bool   `json:"-"`
	Path         string `json:"-"`
}

func (c *Config) Save() error {
	if c.Path == "" {
		c.Path = DefaultConfigPath
	}

	if err := utils.EnsureParent(c.Path); err != nil {
		return err
	}

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(c.Path, data, 0644)
}

func (c *Config) Validate() error {
	var err error
	c.DataDir, err = utils.ResolvePath(c.DataDir)
	if err != nil {
		return fmt.Errorf("`data_dir` is invalid: %w", err)
	}

	if c.Email == "" {
		return fmt.Errorf("`email` is required")
	} else if _, err := mail.ParseAddress(c.Email); err != nil {
		return fmt.Errorf("`email` is invalid: %w", err)
	}

	if c.ServerURL == "" {
		return fmt.Errorf("`server_url` is required")
	} else if _, err := url.Parse(c.ServerURL); err != nil {
		return fmt.Errorf("`server_url` is invalid: %w", err)
	}

	// todo re-enable this once auth is a hard requirement
	// if c.RefreshToken == "" {
	// 	return fmt.Errorf("`refresh_token` is required")
	// }

	return nil
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

	// todo remove override
	cfg.ServerURL = DefaultServerURL

	return &cfg, nil
}
