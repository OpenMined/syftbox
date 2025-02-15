package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/yashgorana/syftbox-go/pkg/server"
)

func main() {
	var certFile string
	var keyFile string

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
					Addr:     server.DefaultAddr,
					CertFile: certFile,
					KeyFile:  keyFile,
				},
				Blob: &server.BlobConfig{
					ServerUrl:  "http://localhost:9000",
					BucketName: "syftgo",
					AccessKey:  "AbH4qZdboOLES93uUUb2",
					SecretKey:  "Pz46w5OYIRO9pAB5urEfyRdSNwLpeQc9CvwguQzX",
					Region:     "us-east-1",
				},
			}
			c, err := server.New(config)
			if err != nil {
				return err
			}
			return c.Start(cmd.Context())
		},
	}

	rootCmd.Flags().StringVar(&certFile, "cert", "", "Path to the certificate file")
	rootCmd.Flags().StringVar(&keyFile, "key", "", "Path to the key file")

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
