package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate_NormalizesAndDefaults(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		DataDir:   tmp,
		Email:     "Alice@Example.com",
		ServerURL: "http://127.0.0.1:8080",
		ClientURL: "http://localhost:7938",
		Path:      filepath.Join(tmp, "config.json"),
	}

	require.NoError(t, cfg.Validate())
	assert.True(t, filepath.IsAbs(cfg.DataDir))
	assert.True(t, filepath.IsAbs(cfg.Path))
	assert.Equal(t, "alice@example.com", cfg.Email)
}

func TestConfig_Validate_ErrorsOnInvalidInputs(t *testing.T) {
	tmp := t.TempDir()

	t.Run("bad email", func(t *testing.T) {
		cfg := &Config{
			DataDir:   tmp,
			Email:     "not-an-email",
			ServerURL: "http://127.0.0.1:8080",
			Path:      filepath.Join(tmp, "config.json"),
		}
		err := cfg.Validate()
		assert.Error(t, err)
	})

	t.Run("bad server url", func(t *testing.T) {
		cfg := &Config{
			DataDir:   tmp,
			Email:     "alice@example.com",
			ServerURL: "ftp://bad.example.com",
			Path:      filepath.Join(tmp, "config.json"),
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server url")
	})

	t.Run("bad client url", func(t *testing.T) {
		cfg := &Config{
			DataDir:   tmp,
			Email:     "alice@example.com",
			ServerURL: "http://127.0.0.1:8080",
			ClientURL: "://bad",
			Path:      filepath.Join(tmp, "config.json"),
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client url")
	})
}

func TestConfig_SaveAndLoad_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")

	cfg := &Config{
		DataDir:      tmp,
		Email:        "alice@example.com",
		ServerURL:    "http://127.0.0.1:8080",
		ClientURL:    "http://localhost:7938",
		ClientToken:  "tok",
		RefreshToken: "rtok",
		AppsEnabled:  false, // should not persist
		AccessToken:  "atok", // should not persist
		Path:         path,
	}

	require.NoError(t, cfg.Validate())
	require.NoError(t, cfg.Save())

	loaded, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, cfg.DataDir, loaded.DataDir)
	assert.Equal(t, cfg.Email, loaded.Email)
	assert.Equal(t, cfg.ServerURL, loaded.ServerURL)
	assert.Equal(t, cfg.ClientURL, loaded.ClientURL)
	assert.Equal(t, cfg.ClientToken, loaded.ClientToken)
	assert.Equal(t, cfg.RefreshToken, loaded.RefreshToken)

	// Non-persisted fields default on load.
	assert.True(t, loaded.AppsEnabled)
	assert.Empty(t, loaded.AccessToken)
	assert.Equal(t, path, loaded.Path)

	// Ensure file exists and is readable.
	_, statErr := os.Stat(path)
	require.NoError(t, statErr)
}

