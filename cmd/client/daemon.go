package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/openmined/syftbox/internal/client"
	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/controlplane"
	"github.com/openmined/syftbox/internal/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newDaemonCmd())
}

func newDaemonCmd() *cobra.Command {
	var addr string
	var authToken string
	var enableSwagger bool

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the SyftBox client daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			slog.Info("syftbox", "version", version.Version, "revision", version.Revision, "build", version.BuildDate)

			// Get config path from flag or environment variable
			// Check if flag was explicitly set
			configPath := ""
			if cmd.Flag("config").Changed {
				configPath = cmd.Flag("config").Value.String()
			} else if envPath := os.Getenv("SYFTBOX_CONFIG_PATH"); envPath != "" {
				// Use environment variable if flag wasn't explicitly set
				configPath = envPath
			} else {
				// Fall back to default
				configPath = config.DefaultConfigPath
			}
			slog.Info("daemon using config", "path", configPath)

			daemon, err := client.NewClientDaemon(&controlplane.CPServerConfig{
				Addr:          addr,
				AuthToken:     authToken,
				EnableSwagger: enableSwagger,
				ConfigPath:    configPath,
			})
			if err != nil {
				return err
			}

			defer slog.Info("Bye!")
			if err := daemon.Start(cmd.Context()); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("daemon start", "error", err)
				return err
			}
			return nil
		},
	}

	daemonCmd.Flags().StringVarP(&addr, "http-addr", "a", "localhost:7938", "Address to bind the local http server")
	daemonCmd.Flags().StringVarP(&authToken, "http-token", "t", "", "Access token for the local http server")
	daemonCmd.Flags().BoolVarP(&enableSwagger, "http-swagger", "s", true, "Enable Swagger for the local http server")

	return daemonCmd
}
