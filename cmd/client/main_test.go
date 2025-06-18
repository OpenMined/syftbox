package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigEnv(t *testing.T) {
	t.Setenv("SYFTBOX_EMAIL", "test@example.com")
	t.Setenv("SYFTBOX_SERVER_URL", "https://test.syftbox.net")
	t.Setenv("SYFTBOX_CLIENT_URL", "http://localhost:7938")
	t.Setenv("SYFTBOX_APPS_ENABLED", "true")
	t.Setenv("SYFTBOX_REFRESH_TOKEN", "test-refresh-token")
	t.Setenv("SYFTBOX_ACCESS_TOKEN", "test-access-token")
	if runtime.GOOS == "windows" {
		t.Setenv("SYFTBOX_DATA_DIR", "C:\\tmp\\syftbox-test")
		t.Setenv("SYFTBOX_CONFIG_PATH", "C:\\tmp\\config.test.json")
	} else {

		t.Setenv("SYFTBOX_DATA_DIR", "/tmp/syftbox-test")
		t.Setenv("SYFTBOX_CONFIG_PATH", "/tmp/config.test.json")
	}

	cfg, err := loadConfig(rootCmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = cfg.Validate()
	require.NoError(t, err)

	assert.Equal(t, "test@example.com", cfg.Email)
	assert.Equal(t, "https://test.syftbox.net", cfg.ServerURL)
	assert.Equal(t, "http://localhost:7938", cfg.ClientURL)
	assert.Equal(t, true, cfg.AppsEnabled)
	assert.Equal(t, "test-refresh-token", cfg.RefreshToken)
	assert.Equal(t, "test-access-token", cfg.AccessToken)

	if runtime.GOOS == "windows" {
		assert.Equal(t, "C:\\tmp\\syftbox-test", cfg.DataDir)
		assert.Equal(t, "C:\\tmp\\config.test.json", cfg.Path)
	} else {
		assert.Equal(t, "/tmp/syftbox-test", cfg.DataDir)
		assert.Equal(t, "/tmp/config.test.json", cfg.Path)
	}
}

func TestLoadConfigJSON(t *testing.T) {
	// Create a temporary JSON file with expected structure
	dummyConfig := `
{
	"email": "test@example.com",
	"data_dir": "/tmp/syftbox-test-json",
	"server_url": "https://test-json.syftbox.net",
	"client_url": "http://localhost:8080",
	"refresh_token": "test-refresh-token-json",
	"access_token": "test-access-token-json"
}
`
	dummyConfigFile := filepath.Join(os.TempDir(), "dummy.json")
	err := os.WriteFile(dummyConfigFile, []byte(dummyConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write dummy config file: %v", err)
	}
	defer os.Remove(dummyConfigFile) // Clean up after test

	rootCmd.PersistentFlags().Set("config", dummyConfigFile)

	// Call buildConfig
	cfg, err := loadConfig(rootCmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Equal(t, dummyConfigFile, cfg.Path)
	assert.Equal(t, "test@example.com", cfg.Email)
	assert.Equal(t, "/tmp/syftbox-test-json", cfg.DataDir)
	assert.Equal(t, "https://test-json.syftbox.net", cfg.ServerURL)
	assert.Equal(t, "http://localhost:8080", cfg.ClientURL)
	assert.Equal(t, "test-refresh-token-json", cfg.RefreshToken)
	assert.Equal(t, "test-access-token-json", cfg.AccessToken) // can read, but not persist!
}
