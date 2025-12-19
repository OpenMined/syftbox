package main

import (
	"context"
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestWatchStatusCommand_FlagsAndDefaults(t *testing.T) {
	watchCmd := newWatchStatusCmdForTest()

	interval := watchCmd.Flags().Lookup("interval")
	require.NotNil(t, interval)
	require.Equal(t, "1s", interval.DefValue)

	raw := watchCmd.Flags().Lookup("raw")
	require.NotNil(t, raw)
	require.Equal(t, "false", raw.DefValue)
}

func TestWatchStatusCommand_ErrorsWhenControlPlaneNotConfigured(t *testing.T) {
	// Ensure it fails before entering the poll loop.
	t.Setenv("SYFTBOX_CLIENT_URL", "")
	t.Setenv("SYFTBOX_CLIENT_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	root := &cobra.Command{Use: "syftbox"}
	root.Flags().StringP("email", "e", "", "your email for your syftbox datasite")
	root.Flags().StringP("datadir", "d", config.DefaultDataDir, "data directory where the syftbox workspace is stored")
	root.Flags().StringP("server", "s", config.DefaultServerURL, "url of the syftbox server")
	root.Flags().String("client-url", config.DefaultClientURL, "control plane URL (host:port)")
	root.Flags().String("client-token", "", "control plane access token")
	root.PersistentFlags().StringP("config", "c", config.DefaultConfigPath, "path to config file")

	root.AddCommand(newWatchStatusCmdForTest())
	root.SetContext(ctx)
	root.SetArgs([]string{"watch-status"})

	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "client control plane not configured")
}

func newWatchStatusCmdForTest() *cobra.Command {
	cmd := &cobra.Command{
		Use:   watchStatusCmd.Use,
		Short: watchStatusCmd.Short,
		RunE:  watchStatusCmd.RunE,
	}
	cmd.Flags().Duration("interval", 1*time.Second, "poll interval")
	cmd.Flags().Bool("raw", false, "print raw json without pretty formatting")
	return cmd
}
