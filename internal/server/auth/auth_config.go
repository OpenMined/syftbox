package auth

import (
	"fmt"
	"time"
)

type Config struct {
	Enabled            bool          `mapstructure:"enabled"`
	TokenIssuer        string        `mapstructure:"token_issuer"`
	RefreshTokenSecret string        `mapstructure:"refresh_token_secret"`
	RefreshTokenExpiry time.Duration `mapstructure:"refresh_token_expiry"`
	AccessTokenSecret  string        `mapstructure:"access_token_secret"`
	AccessTokenExpiry  time.Duration `mapstructure:"access_token_expiry"`
	EmailOTPLength     int           `mapstructure:"email_otp_length"`
	EmailOTPExpiry     time.Duration `mapstructure:"email_otp_expiry"`
}

func (c *Config) Validate() error {
	// Validate Auth config if enabled
	if c.Enabled {
		if c.TokenIssuer == "" {
			return fmt.Errorf("auth `token_issuer` is required when auth is enabled")
		}
		if c.RefreshTokenSecret == "" {
			return fmt.Errorf("auth `refresh_token_secret` is required when auth is enabled")
		}
		if c.AccessTokenSecret == "" {
			return fmt.Errorf("auth `access_token_secret` is required when auth is enabled")
		}
		if c.EmailOTPLength < 6 {
			return fmt.Errorf("auth `email_otp_length` must be greater than 6")
		}
	}
	return nil
}
