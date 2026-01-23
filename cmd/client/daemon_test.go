package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonConfigPath(t *testing.T) {
	// Helper function to create a test daemon command
	createTestDaemonCmd := func() *cobra.Command {
		var addr string
		var authToken string
		var enableSwagger bool

		cmd := &cobra.Command{
			Use:   "daemon",
			Short: "Test daemon command",
			RunE: func(cmd *cobra.Command, args []string) error {
				cmd.Annotations = map[string]string{
					"resolved_config": resolveConfigPath(cmd),
				}
				return nil
			},
		}

		cmd.Flags().StringVarP(&addr, "http-addr", "a", "localhost:7938", "Address to bind")
		cmd.Flags().StringVarP(&authToken, "http-token", "t", "", "Access token")
		cmd.Flags().BoolVarP(&enableSwagger, "http-swagger", "s", true, "Enable Swagger")
		cmd.PersistentFlags().StringP("config", "c", config.DefaultConfigPath, "path to config file")

		return cmd
	}

	t.Run("uses SYFTBOX_CONFIG_PATH environment variable", func(t *testing.T) {
		// Set environment variable
		testPath := "/custom/env/path/config.json"
		os.Setenv("SYFTBOX_CONFIG_PATH", testPath)
		defer os.Unsetenv("SYFTBOX_CONFIG_PATH")

		cmd := createTestDaemonCmd()
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		require.NoError(t, err)

		// Check resolved config path
		assert.Equal(t, testPath, cmd.Annotations["resolved_config"])
	})

	t.Run("flag overrides environment variable", func(t *testing.T) {
		// Set environment variable
		envPath := "/env/path/config.json"
		os.Setenv("SYFTBOX_CONFIG_PATH", envPath)
		defer os.Unsetenv("SYFTBOX_CONFIG_PATH")

		flagPath := "/flag/path/config.json"
		cmd := createTestDaemonCmd()
		cmd.SetArgs([]string{"--config", flagPath})

		err := cmd.Execute()
		require.NoError(t, err)

		// Flag should override env var
		assert.Equal(t, flagPath, cmd.Annotations["resolved_config"])
	})

	t.Run("uses default when no env or flag", func(t *testing.T) {
		// Make sure env var is not set
		os.Unsetenv("SYFTBOX_CONFIG_PATH")

		cmd := createTestDaemonCmd()
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		require.NoError(t, err)

		// Should use default
		home, _ := os.UserHomeDir()
		expectedDefault := filepath.Join(home, ".syftbox", "config.json")
		assert.Equal(t, expectedDefault, cmd.Annotations["resolved_config"])
	})

	t.Run("short flag -c works", func(t *testing.T) {
		os.Unsetenv("SYFTBOX_CONFIG_PATH")

		flagPath := "/short/flag/config.json"
		cmd := createTestDaemonCmd()
		cmd.SetArgs([]string{"-c", flagPath})

		err := cmd.Execute()
		require.NoError(t, err)

		assert.Equal(t, flagPath, cmd.Annotations["resolved_config"])
	})

	t.Run("priority order: flag > env > default", func(t *testing.T) {
		// This test documents the expected priority order
		envPath := "/env/config.json"
		flagPath := "/flag/config.json"

		// Test 1: Only env var set
		os.Setenv("SYFTBOX_CONFIG_PATH", envPath)
		cmd1 := createTestDaemonCmd()
		cmd1.SetArgs([]string{})
		err := cmd1.Execute()
		require.NoError(t, err)
		assert.Equal(t, envPath, cmd1.Annotations["resolved_config"], "Should use env var when no flag")

		// Test 2: Both env var and flag set
		cmd2 := createTestDaemonCmd()
		cmd2.SetArgs([]string{"--config", flagPath})
		err = cmd2.Execute()
		require.NoError(t, err)
		assert.Equal(t, flagPath, cmd2.Annotations["resolved_config"], "Flag should override env var")

		// Test 3: Neither set
		os.Unsetenv("SYFTBOX_CONFIG_PATH")
		cmd3 := createTestDaemonCmd()
		cmd3.SetArgs([]string{})
		err = cmd3.Execute()
		require.NoError(t, err)
		home, _ := os.UserHomeDir()
		expectedDefault := filepath.Join(home, ".syftbox", "config.json")
		assert.Equal(t, expectedDefault, cmd3.Annotations["resolved_config"], "Should use default when nothing set")

		// Clean up
		os.Unsetenv("SYFTBOX_CONFIG_PATH")
	})
}
