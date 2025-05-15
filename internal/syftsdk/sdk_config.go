package syftsdk

import (
	"errors"

	"github.com/openmined/syftbox/internal/utils"
)

const (
	DefaultBaseURL = "https://syftboxdev.openmined.org"
)

var (
	ErrNoRefreshToken = errors.New("refresh token is missing")
	ErrNoServerURL    = errors.New("server URL is missing")
	ErrInvalidOTP     = errors.New("invalid OTP")
	ErrInvalidEmail   = errors.New("invalid email")
)

// SyftSDKConfig is the configuration for the SyftSDK
type SyftSDKConfig struct {
	BaseURL      string // BaseURL is required
	Email        string // Email is required
	RefreshToken string // RefreshToken is required
	AccessToken  string // AccessToken is optional
}

func (c *SyftSDKConfig) Validate() error {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}

	if c.RefreshToken == "" {
		return ErrNoRefreshToken
	}

	if err := utils.ValidateEmail(c.Email); err != nil {
		return err
	}

	return nil
}
