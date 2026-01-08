package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLoadConfigTestCmd(t *testing.T) *cobra.Command {
	t.Helper()

	// Ensure we never read the developer's real config from their home directory.
	oldHome := home
	home = t.TempDir()
	t.Cleanup(func() { home = oldHome })

	cmd := &cobra.Command{}
	cmd.Flags().StringP("email", "e", "", "")
	cmd.Flags().StringP("datadir", "d", config.DefaultDataDir, "")
	cmd.Flags().StringP("server", "s", config.DefaultServerURL, "")
	cmd.Flags().String("client-url", config.DefaultClientURL, "")
	cmd.Flags().String("client-token", "", "")
	cmd.PersistentFlags().StringP("config", "c", filepath.Join(home, ".syftbox", "config.json"), "")
	return cmd
}

func TestLoadConfigEnv(t *testing.T) {
	cmd := newLoadConfigTestCmd(t)
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

	cfg, err := loadConfig(cmd)
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
	cmd := newLoadConfigTestCmd(t)
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

	cmd.PersistentFlags().Set("config", dummyConfigFile)

	// Call buildConfig
	cfg, err := loadConfig(cmd)
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

func TestLoadConfigPrecedence_FlagBeatsEnvBeatsFile(t *testing.T) {
	cmd := newLoadConfigTestCmd(t)

	// Config file establishes baseline values.
	fileCfg := `{
  "email": "file@example.com",
  "data_dir": "/tmp/syftbox-file",
  "server_url": "https://file.syftbox.net",
  "client_url": "http://file.local:1234",
  "client_token": "file-token"
}`
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fileCfg), 0o644))
	require.NoError(t, cmd.PersistentFlags().Set("config", cfgPath))

	// Env should override file when flags are not set.
	t.Setenv("SYFTBOX_EMAIL", "env@example.com")
	t.Setenv("SYFTBOX_DATA_DIR", "/tmp/syftbox-env")
	t.Setenv("SYFTBOX_SERVER_URL", "https://env.syftbox.net")
	t.Setenv("SYFTBOX_CLIENT_URL", "http://env.local:5555")
	t.Setenv("SYFTBOX_CLIENT_TOKEN", "env-token")

	cfg, err := loadConfig(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "env@example.com", cfg.Email)
	require.Equal(t, "/tmp/syftbox-env", cfg.DataDir)
	require.Equal(t, "https://env.syftbox.net", cfg.ServerURL)
	require.Equal(t, "http://env.local:5555", cfg.ClientURL)
	require.Equal(t, "env-token", cfg.ClientToken)

	// Flags should override env + file.
	require.NoError(t, cmd.Flags().Set("email", "flag@example.com"))
	require.NoError(t, cmd.Flags().Set("datadir", "/tmp/syftbox-flag"))
	require.NoError(t, cmd.Flags().Set("server", "https://flag.syftbox.net"))
	require.NoError(t, cmd.Flags().Set("client-url", "http://flag.local:9999"))
	require.NoError(t, cmd.Flags().Set("client-token", "flag-token"))

	cfg, err = loadConfig(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "flag@example.com", cfg.Email)
	require.Equal(t, "/tmp/syftbox-flag", cfg.DataDir)
	require.Equal(t, "https://flag.syftbox.net", cfg.ServerURL)
	require.Equal(t, "http://flag.local:9999", cfg.ClientURL)
	require.Equal(t, "flag-token", cfg.ClientToken)
}

func TestLoadConfigRejectsLegacyServer(t *testing.T) {
	cmd := newLoadConfigTestCmd(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	require.NoError(
		t,
		os.WriteFile(
			cfgPath,
			[]byte(`{"email":"alice@example.com","data_dir":"/tmp/syftbox","server_url":"https://openmined.org"}`),
			0o644,
		),
	)
	require.NoError(t, cmd.PersistentFlags().Set("config", cfgPath))

	_, err := loadConfig(cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "legacy server detected")
}

func TestLoadConfigSearchesHomeConfigPaths(t *testing.T) {
	cmd := newLoadConfigTestCmd(t)

	cfgPath := filepath.Join(home, ".syftbox", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(
		t,
		os.WriteFile(
			cfgPath,
			[]byte(`{"email":"alice@example.com","data_dir":"/tmp/syftbox","server_url":"https://syftbox.net"}`),
			0o644,
		),
	)

	// Ensure loadConfig uses the search paths by NOT setting the --config flag.
	_, err := cmd.PersistentFlags().GetString("config")
	require.NoError(t, err)

	cfg, err := loadConfig(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "alice@example.com", cfg.Email)
	require.Equal(t, "https://syftbox.net", cfg.ServerURL)
	// viper/config unmarshal should not depend on the file having an explicit client_url
	require.NotEmpty(t, cfg.ClientURL)
}

func TestLoadConfigSearchPathsIgnoreMissingFile(t *testing.T) {
	cmd := newLoadConfigTestCmd(t)

	// When no config is found, loadConfig should still succeed using defaults/env.
	_, err := os.Stat(filepath.Join(home, ".syftbox", "config.json"))
	require.True(t, errors.Is(err, os.ErrNotExist))

	t.Setenv("SYFTBOX_EMAIL", "env@example.com")
	t.Setenv("SYFTBOX_DATA_DIR", "/tmp/syftbox-env")
	t.Setenv("SYFTBOX_SERVER_URL", "https://env.syftbox.net")

	cfg, err := loadConfig(cmd)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "env@example.com", cfg.Email)
	require.Equal(t, "/tmp/syftbox-env", cfg.DataDir)
	require.Equal(t, "https://env.syftbox.net", cfg.ServerURL)
}
