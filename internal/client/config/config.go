package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
)

var (
	home, _            = os.UserHomeDir()
	DefaultConfigPath  = filepath.Join(home, ".syftbox", "config.json")
	DefaultServerURL   = "https://syftboxdev.openmined.org"
	DefaultClientURL   = "http://localhost:7938"
	DefaultLogFilePath = filepath.Join(home, ".syftbox", "logs", "SyftBoxDaemon.log")
)

var (
	ErrInvalidURL     = errors.New("invalid url")
	ErrNoRefreshToken = errors.New("credentials missing")
	ErrInvalidEmail   = utils.ErrInvalidEmail
)

type Config struct {
	DataDir      string `json:"data_dir"`
	Email        string `json:"email"`
	ServerURL    string `json:"server_url"`
	ClientURL    string `json:"client_url,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AccessToken  string `json:"-"` // must never be persisted. always in memory
	AppsEnabled  bool   `json:"-"`
	Path         string `json:"-"`
}

func (c *Config) Save() error {
	if err := utils.EnsureParent(c.Path); err != nil {
		return err
	}

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(c.Path, data, 0o644)
}

func (c *Config) Validate() error {
	if c.Path == "" {
		c.Path = DefaultConfigPath
	}

	var err error
	c.DataDir, err = utils.ResolvePath(c.DataDir)
	if err != nil {
		return err
	}

	if err := utils.ValidateEmail(c.Email); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEmail, err)
	}
	c.Email = strings.ToLower(c.Email)

	if err := validateURL(c.ServerURL); err != nil {
		return fmt.Errorf("invalid server url: %w", err)
	}

	if err := validateURL(c.ClientURL); err != nil {
		return fmt.Errorf("invalid client url: %w", err)
	}

	if c.RefreshToken == "" {
		return ErrNoRefreshToken
	}

	return nil
}

func LoadFromFile(path string) (*Config, error) {
	path, err := utils.ResolvePath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer data.Close()

	return LoadFromReader(path, data)
}

func LoadFromReader(path string, reader io.ReadCloser) (*Config, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	cfg.Path = path
	cfg.AppsEnabled = true

	return &cfg, nil
}

func validateURL(urlString string) error {
	if urlString == "" {
		return fmt.Errorf("%w '%s'", ErrInvalidURL, urlString)
	} else if _, err := url.ParseRequestURI(urlString); err != nil {
		return fmt.Errorf("%w '%s'", ErrInvalidURL, urlString)
	}
	return nil
}
