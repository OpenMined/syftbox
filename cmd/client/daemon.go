package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/openmined/syftbox/internal/client"
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
			slog.Info("syftbox", "version", version.Version, "revision", version.Revision, "build", version.BuildDate)

			daemon, err := client.NewClientDaemon(&client.ControlPlaneConfig{
				Addr:          addr,
				AuthToken:     authToken,
				EnableSwagger: enableSwagger,
			})
			if err != nil {
				return err
			}

			defer slog.Info("Bye!")
			if err := daemon.Start(cmd.Context()); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("start daemon", "error", err)
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
