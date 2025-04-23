package main

import (
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/yashgorana/syftbox-go/internal/client"
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
			daemon, err := client.NewClientDaemon(&client.ControlPlaneConfig{
				Addr:          addr,
				AuthToken:     authToken,
				EnableSwagger: enableSwagger,
			})
			if err != nil {
				return err
			}

			defer slog.Info("Bye!")
			return daemon.Start(cmd.Context())
		},
	}

	daemonCmd.Flags().StringVarP(&addr, "http-addr", "a", "localhost:7938", "Address to bind the local http server")
	daemonCmd.Flags().StringVarP(&authToken, "http-token", "t", "", "Access token for the local http server")
	daemonCmd.Flags().BoolVarP(&enableSwagger, "http-swagger", "s", true, "Enable Swagger for the local http server")

	return daemonCmd
}
