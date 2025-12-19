package syftsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyftSDKConfig_Validate(t *testing.T) {
	t.Run("valid config passes", func(t *testing.T) {
		cfg := &SyftSDKConfig{
			BaseURL:      "http://127.0.0.1:8080",
			Email:        "alice@example.com",
			RefreshToken: "rtok",
		}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("invalid email fails", func(t *testing.T) {
		cfg := &SyftSDKConfig{
			BaseURL: "http://127.0.0.1:8080",
			Email:   "not-an-email",
		}
		assert.ErrorIs(t, cfg.Validate(), ErrInvalidEmail)
	})

	t.Run("missing base url fails", func(t *testing.T) {
		cfg := &SyftSDKConfig{
			BaseURL: "",
			Email:   "alice@example.com",
		}
		assert.ErrorIs(t, cfg.Validate(), ErrNoServerURL)
	})
}

