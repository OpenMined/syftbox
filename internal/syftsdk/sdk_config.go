package syftsdk

import (
	"github.com/openmined/syftbox/internal/utils"
)

const (
	DefaultBaseURL = "https://syftbox.net"
)

// SyftSDKConfig is the configuration for the SyftSDK
type SyftSDKConfig struct {
	BaseURL      string // BaseURL is required
	Email        string // Email is required
	RefreshToken string // RefreshToken is required
	AccessToken  string // AccessToken is optional
}

func (c *SyftSDKConfig) Validate() error {
	if !utils.IsValidEmail(c.Email) {
		return ErrInvalidEmail
	}

	if c.BaseURL == "" {
		return ErrNoServerURL
	}

	if err := utils.ValidateEmail(c.Email); err != nil {
		return err
	}

	return nil
}
