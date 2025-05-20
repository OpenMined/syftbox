package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate_Valid(t *testing.T) {
	cfg := &Config{
		Enabled:            true,
		TokenIssuer:        "https://issuer.com",
		RefreshTokenSecret: "refresh",
		AccessTokenSecret:  "access",
		RefreshTokenExpiry: time.Hour,
		AccessTokenExpiry:  time.Minute,
		EmailAddr:          "test@email.com",
		EmailOTPLength:     6,
		EmailOTPExpiry:     time.Minute,
	}
	err := cfg.Validate()
	require.NoError(t, err)
}

func TestConfigValidate_InvalidURL(t *testing.T) {
	cfg := &Config{
		Enabled:            true,
		TokenIssuer:        "not-a-url",
		RefreshTokenSecret: "refresh",
		AccessTokenSecret:  "access",
		EmailAddr:          "test@email.com",
		EmailOTPLength:     6,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token_issuer")
}

func TestConfigValidate_MissingSecrets(t *testing.T) {
	cfg := &Config{
		Enabled:        true,
		TokenIssuer:    "https://issuer.com",
		EmailAddr:      "test@email.com",
		EmailOTPLength: 6,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refresh_token_secret")

	cfg.RefreshTokenSecret = "refresh"
	err = cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_token_secret")
}

func TestConfigValidate_ShortOTPLength(t *testing.T) {
	cfg := &Config{
		Enabled:            true,
		TokenIssuer:        "https://issuer.com",
		RefreshTokenSecret: "refresh",
		AccessTokenSecret:  "access",
		EmailAddr:          "test@email.com",
		EmailOTPLength:     4,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email_otp_length")
}

func TestConfigValidate_InvalidEmail(t *testing.T) {
	cfg := &Config{
		Enabled:            true,
		TokenIssuer:        "https://issuer.com",
		RefreshTokenSecret: "refresh",
		AccessTokenSecret:  "access",
		EmailAddr:          "not-an-email",
		EmailOTPLength:     6,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sender email")
}

func TestConfigValidate_Disabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	err := cfg.Validate()
	require.NoError(t, err)
}
