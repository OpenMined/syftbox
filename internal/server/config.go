package server

import (
	"fmt"

	"github.com/openmined/syftbox/internal/server/auth"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/utils"
)

// Config holds the overall server configuration.
type Config struct {
	HTTP    HTTPConfig    `mapstructure:"http"`
	Blob    blob.S3Config `mapstructure:"blob"`
	Auth    auth.Config   `mapstructure:"auth"`
	DataDir string        `mapstructure:"data_dir"`
}

// Validate checks the configuration for essential values and consistency.
func (c *Config) Validate() error {
	var err error
	c.DataDir, err = utils.ResolvePath(c.DataDir)
	if err != nil {
		return fmt.Errorf("invalid data directory: %w", err)
	}

	if err := c.HTTP.Validate(); err != nil {
		return fmt.Errorf("invalid http config: %w", err)
	}

	if err := c.Blob.Validate(); err != nil {
		return fmt.Errorf("invalid blob config: %w", err)
	}

	if err := c.Auth.Validate(); err != nil {
		return fmt.Errorf("invalid auth config: %w", err)
	}

	return nil
}

// HTTPConfig holds HTTP server specific configuration.
type HTTPConfig struct {
	Addr     string `mapstructure:"addr"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

func (c *HTTPConfig) HasCerts() bool {
	return c.CertFile != "" && c.KeyFile != ""
}

func (c *HTTPConfig) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("http addr required")
	}
	if (c.CertFile != "" && c.KeyFile == "") || (c.CertFile == "" && c.KeyFile != "") {
		return fmt.Errorf("cert_file and key_file required together")
	}
	return nil
}
