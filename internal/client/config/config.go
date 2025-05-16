package config

import (
	"encoding/json"
	"errors"
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
	DefaultClientURL   = "http://localhost:8080"
	DefaultLogFilePath = filepath.Join(home, ".syftbox", "logs", "SyftBoxDaemon.log")
)

var (
	ErrServerURLEmpty   = errors.New("`server url` is empty")
	ErrServerURLInvalid = errors.New("`server url` is not valid")
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
		return err
	}
	c.Email = strings.ToLower(c.Email)

	if err := validateURL(c.ServerURL); err != nil {
		return err
	}

	// todo re-enable this once auth is a hard requirement
	// if c.RefreshToken == "" {
	// 	return fmt.Errorf("`refresh_token` is required")
	// }

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
		return ErrServerURLEmpty
	} else if _, err := url.Parse(urlString); err != nil {
		return ErrServerURLInvalid
	}
	return nil
}
