package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.PersistentFlags().StringP("config", "c", config.DefaultConfigPath, "path to config file")
	return cmd
}

func TestResolveConfigPathFlagBeatsEnv(t *testing.T) {
	cmd := newTestCmd()
	flagPath := "/tmp/flag/config.json"
	envPath := "/tmp/env/config.json"

	t.Setenv("SYFTBOX_CONFIG_PATH", envPath)
	err := cmd.PersistentFlags().Set("config", flagPath)
	assert.NoError(t, err)

	resolved := resolveConfigPath(cmd)
	assert.Equal(t, flagPath, resolved)
}

func TestResolveConfigPathUsesEnvWhenNoFlag(t *testing.T) {
	cmd := newTestCmd()
	envPath := "/tmp/env/config.json"

	t.Setenv("SYFTBOX_CONFIG_PATH", envPath)

	resolved := resolveConfigPath(cmd)
	assert.Equal(t, envPath, resolved)
}

func TestResolveConfigPathFindsExistingFile(t *testing.T) {
	// Point the helper's home to an isolated temp dir so we don't touch real files.
	oldHome := home
	tempHome := t.TempDir()
	home = tempHome
	t.Cleanup(func() { home = oldHome })

	cmd := newTestCmd()
	t.Setenv("SYFTBOX_CONFIG_PATH", "")

	existing := filepath.Join(home, ".config", "syftbox", "config.json")
	err := os.MkdirAll(filepath.Dir(existing), 0o755)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(existing, []byte("{}"), 0o644))

	resolved := resolveConfigPath(cmd)
	assert.Equal(t, existing, resolved)
}

func TestResolveConfigPathFallsBackToDefault(t *testing.T) {
	cmd := newTestCmd()
	t.Setenv("SYFTBOX_CONFIG_PATH", "")

	resolved := resolveConfigPath(cmd)
	assert.Equal(t, config.DefaultConfigPath, resolved)
}
