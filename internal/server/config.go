package server

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/openmined/syftbox/internal/server/auth"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/email"
	"github.com/openmined/syftbox/internal/utils"
)

// Config holds the overall server configuration.
type Config struct {
	HTTP    HTTPConfig    `mapstructure:"http"`
	Blob    blob.S3Config `mapstructure:"blob"`
	Auth    auth.Config   `mapstructure:"auth"`
	Email   email.Config  `mapstructure:"email"`
	DataDir string        `mapstructure:"data_dir"`
	LogDir  string        `mapstructure:"log_dir"`
}

// LogValue for Config
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("data_dir", c.DataDir),
		slog.String("log_dir", c.LogDir),
		slog.Any("http", c.HTTP),
		slog.Any("blob", c.Blob),
		slog.Any("auth", c.Auth),
		slog.Any("email", c.Email),
	)
}

// Validate checks the configuration for essential values and consistency.
func (c *Config) Validate() error {
	var err error
	c.DataDir, err = utils.ResolvePath(c.DataDir)
	if err != nil {
		return fmt.Errorf("invalid data directory: %w", err)
	}

	// Resolve LogDir path
	if c.LogDir == "" {
		// Default to .logs in the current working directory if not set
		c.LogDir = ".logs"
	}
	c.LogDir, err = utils.ResolvePath(c.LogDir)
	if err != nil {
		return fmt.Errorf("invalid log directory: %w", err)
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

	if err := c.Email.Validate(); err != nil {
		return fmt.Errorf("invalid email config: %w", err)
	}

	return nil
}

// HTTPConfig holds HTTP server specific configuration.
type HTTPConfig struct {
	Addr              string        `mapstructure:"addr"`
	CertFilePath      string        `mapstructure:"cert_file"`
	KeyFilePath       string        `mapstructure:"key_file"`
	Domain            string        `mapstructure:"domain"` // Main domain for subdomain routing (e.g., "syftbox.net")
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
}

// LogValue for HTTPConfig
func (hc HTTPConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("address", hc.Addr),
		slog.String("cert_file", hc.CertFilePath),
		slog.String("key_file", hc.KeyFilePath),
		slog.String("domain", hc.Domain),
		slog.Duration("read_timeout", hc.ReadTimeout),
		slog.Duration("write_timeout", hc.WriteTimeout),
		slog.Duration("idle_timeout", hc.IdleTimeout),
		slog.Duration("read_header_timeout", hc.ReadHeaderTimeout),
	)
}

func (c *HTTPConfig) HTTPSEnabled() bool {
	return c.CertFilePath != "" && c.KeyFilePath != ""
}

func (c *HTTPConfig) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("http addr required")
	}
	if (c.CertFilePath != "" && c.KeyFilePath == "") || (c.CertFilePath == "" && c.KeyFilePath != "") {
		return fmt.Errorf("cert_file and key_file paths are required together")
	}

	// Apply defaults when not provided to avoid unexpectedly short timeouts for large uploads
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 5 * time.Minute
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 15 * time.Minute
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 15 * time.Minute
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = 15 * time.Second
	}
	return nil
}
