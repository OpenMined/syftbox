package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yashgorana/syftbox-go/internal/blob"
	"github.com/yashgorana/syftbox-go/internal/server"
	"github.com/yashgorana/syftbox-go/internal/version"
)

var (
	// Default config values from dev.yaml
	defaultBlobBucket    = "syftbox-local"
	defaultBlobRegion    = "us-east-1"
	defaultBlobEndpoint  = "http://localhost:9000"
	defaultBlobAccessKey = "ptSLdKiwOi2LYQFZYEZ6"
	defaultBlobSecretKey = "GMDvYrAhWDkB2DyFMn8gU8I8Bg0fT3JGT6iEB7P8"
)

func main() {
	var certFile string
	var keyFile string
	var addr string
	var configFile string
	var showVersion bool

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

	// Initialize Viper
	viper.SetConfigType("yaml")

	// Set up environment variables
	viper.SetEnvPrefix("SYFTBOX")
	viper.AutomaticEnv()

	viper.SetDefault("BLOB_BUCKET", defaultBlobBucket)
	viper.SetDefault("BLOB_REGION", defaultBlobRegion)
	viper.SetDefault("BLOB_ENDPOINT", defaultBlobEndpoint)
	viper.SetDefault("BLOB_ACCESS_KEY", defaultBlobAccessKey)
	viper.SetDefault("BLOB_SECRET_KEY", defaultBlobSecretKey)

	var rootCmd = &cobra.Command{
		Use:     "server",
		Short:   "SyftBox Server CLI",
		Version: version.Detailed(),
		PreRun: func(cmd *cobra.Command, args []string) {
			if showVersion {
				cmd.Println(version.Detailed())
				os.Exit(0)
			}
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if configFile != "" {
				viper.SetConfigFile(configFile)
				if err := viper.ReadInConfig(); err != nil {
					return err
				}
				slog.Info("Using config file", "file", viper.ConfigFileUsed())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			config := &server.Config{
				Http: &server.HttpServerConfig{
					Addr:     addr,
					CertFile: certFile,
					KeyFile:  keyFile,
				},
				Blob: &blob.BlobConfig{
					BucketName: viper.GetString("BLOB_BUCKET"),
					Region:     viper.GetString("BLOB_REGION"),
					Endpoint:   viper.GetString("BLOB_ENDPOINT"),
					AccessKey:  viper.GetString("BLOB_ACCESS_KEY"),
					SecretKey:  viper.GetString("BLOB_SECRET_KEY"),
				},
			}

			// Log all configuration details
			slog.Info("Server configuration loaded",
				"http.addr", config.Http.Addr,
				"http.cert_file", config.Http.CertFile,
				"http.key_file", config.Http.KeyFile,
				"blob.bucket_name", config.Blob.BucketName,
				"blob.region", config.Blob.Region,
				"blob.endpoint", config.Blob.Endpoint,
				"blob.access_key", maskSecret(config.Blob.AccessKey),
				"blob.secret_key", maskSecret(config.Blob.SecretKey),
			)

			c, err := server.New(config)
			if err != nil {
				slog.Error("Failed to create server", "error", err)
				return err
			}

			slog.Info("Server created successfully, starting...")
			defer slog.Info("Bye!")
			return c.Start(cmd.Context())
		},
	}

	rootCmd.Flags().StringVarP(&certFile, "cert", "c", "", "Path to the certificate file")
	rootCmd.Flags().StringVarP(&keyFile, "key", "k", "", "Path to the key file")
	rootCmd.Flags().StringVarP(&addr, "bind", "b", server.DefaultAddr, "Address to bind the server")
	rootCmd.Flags().StringVarP(&configFile, "config", "f", "", "Path to config file")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Show version information")

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

// maskSecret returns first 4 chars of secret followed by "***"
func maskSecret(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "***"
}
