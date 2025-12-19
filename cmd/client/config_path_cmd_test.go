package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestConfigPathCommand_PrintsResolvedPath(t *testing.T) {
	cmd := &cobra.Command{Use: "syftbox"}
	cmd.PersistentFlags().StringP("config", "c", config.DefaultConfigPath, "path to config file")
	cmd.AddCommand(newConfigPathCmd())

	// Ensure env isn't influencing this test.
	t.Setenv("SYFTBOX_CONFIG_PATH", "")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"config-path"})

	require.NoError(t, cmd.Execute())
	require.Equal(t, config.DefaultConfigPath, strings.TrimSpace(out.String()))
}

