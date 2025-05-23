package auth

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/openmined/syftbox/internal/utils"
)

type Config struct {
	Enabled            bool          `mapstructure:"enabled"`
	TokenIssuer        string        `mapstructure:"token_issuer"`
	RefreshTokenSecret string        `mapstructure:"refresh_token_secret"`
	RefreshTokenExpiry time.Duration `mapstructure:"refresh_token_expiry"`
	AccessTokenSecret  string        `mapstructure:"access_token_secret"`
	AccessTokenExpiry  time.Duration `mapstructure:"access_token_expiry"`
	EmailAddr          string        `mapstructure:"email_addr"`
	EmailOTPLength     int           `mapstructure:"email_otp_length"`
	EmailOTPExpiry     time.Duration `mapstructure:"email_otp_expiry"`
}

func (c *Config) Validate() error {
	// Validate Auth config if enabled
	if c.Enabled {
		if !utils.IsValidURL(c.TokenIssuer) {
			return fmt.Errorf("invalid token_issuer %q", c.TokenIssuer)
		}
		if c.RefreshTokenSecret == "" {
			return fmt.Errorf("refresh_token_secret required")
		}
		if c.AccessTokenSecret == "" {
			return fmt.Errorf("access_token_secret required")
		}
		if c.EmailOTPLength < 6 {
			return fmt.Errorf("email_otp_length must be >= 6")
		}
		if !utils.IsValidEmail(c.EmailAddr) {
			return fmt.Errorf("invalid sender email %q", c.EmailAddr)
		}
	}
	return nil
}

func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enabled", c.Enabled),
		slog.String("token_issuer", c.TokenIssuer),
		slog.String("refresh_token_secret", utils.MaskSecret(c.RefreshTokenSecret)),
		slog.Duration("refresh_token_expiry", c.RefreshTokenExpiry),
		slog.String("access_token_secret", utils.MaskSecret(c.AccessTokenSecret)),
		slog.Duration("access_token_expiry", c.AccessTokenExpiry),
		slog.String("email_addr", c.EmailAddr),
		slog.Int("email_otp_length", c.EmailOTPLength),
		slog.Duration("email_otp_expiry", c.EmailOTPExpiry),
	)
}
