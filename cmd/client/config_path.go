package main

import (
	"os"
	"path/filepath"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/spf13/cobra"
)

// resolveConfigPath determines which config file path to use, honoring (in order):
// 1) An explicitly set --config flag
// 2) SYFTBOX_CONFIG_PATH environment variable
// 3) Existing config files in common locations
// 4) The default path
func resolveConfigPath(cmd *cobra.Command) string {
	if cfgFlag := cmd.Flag("config"); cfgFlag != nil && cfgFlag.Changed {
		return cfgFlag.Value.String()
	}

	if envPath := os.Getenv("SYFTBOX_CONFIG_PATH"); envPath != "" {
		return envPath
	}

	candidates := []string{
		config.DefaultConfigPath,
		filepath.Join(home, ".config", "syftbox", "config.json"),
	}

	for _, candidate := range candidates {
		if utils.FileExists(candidate) {
			return candidate
		}
	}

	return config.DefaultConfigPath
}
