package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/yashgorana/syftbox-go/pkg/blob"
	"github.com/yashgorana/syftbox-go/pkg/server"
)

func main() {
	var certFile string
	var keyFile string
	var addr string

	// Setup logger
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Setup root context with signal handling
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var rootCmd = &cobra.Command{
		Use:   "server",
		Short: "SyftBox Server CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			config := &server.Config{
				Http: &server.HttpServerConfig{
					Addr:     addr,
					CertFile: certFile,
					KeyFile:  keyFile,
				},
				Blob: &blob.BlobStorageConfig{
					BucketName: "syftgo",
					Region:     "us-east-1",
					Endpoint:   "http://localhost:9000",
					AccessKey:  "AbH4qZdboOLES93uUUb2",
					SecretKey:  "Pz46w5OYIRO9pAB5urEfyRdSNwLpeQc9CvwguQzX",
				},
			}
			c, err := server.New(config)
			if err != nil {
				return err
			}
			defer slog.Info("Bye!")
			return c.Start(cmd.Context())
		},
	}

	rootCmd.Flags().StringVarP(&certFile, "cert", "c", "", "Path to the certificate file")
	rootCmd.Flags().StringVarP(&keyFile, "key", "k", "", "Path to the key file")
	rootCmd.Flags().StringVarP(&addr, "bind", "b", server.DefaultAddr, "Address to bind the server")

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
