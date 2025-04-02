package main

import (
	"context"
	"fmt"
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

const (
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

	var rootCmd = &cobra.Command{
		Use:     "server",
		Short:   "SyftBox Server CLI",
		Version: version.Detailed(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			config := &server.Config{
				Http: &server.HttpServerConfig{
					Addr:     addr,
					CertFile: certFile,
					KeyFile:  keyFile,
				},
				Blob: &blob.BlobConfig{
					BucketName: viper.GetString("blob.bucket_name"),
					Region:     viper.GetString("blob.region"),
					Endpoint:   viper.GetString("blob.endpoint"),
					AccessKey:  viper.GetString("blob.access_key"),
					SecretKey:  viper.GetString("blob.secret_key"),
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

func loadConfig(cmd *cobra.Command) error {
	if cmd.Flag("config").Changed {
		configFilePath, _ := cmd.Flags().GetString("config")
		viper.SetConfigFile(configFilePath)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.SetDefault("blob.bucket_name", defaultBlobBucket)
	viper.SetDefault("blob.region", defaultBlobRegion)
	viper.SetDefault("blob.endpoint", defaultBlobEndpoint)
	viper.SetDefault("blob.access_key", defaultBlobAccessKey)
	viper.SetDefault("blob.secret_key", defaultBlobSecretKey)

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Set up environment variables
	viper.SetEnvPrefix("SYFTBOX")
	viper.AutomaticEnv()

	return nil
}

// maskSecret returns first 4 chars of secret followed by "***"
func maskSecret(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "***"
}
