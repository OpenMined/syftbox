package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/openmined/syftbox/internal/server"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/openmined/syftbox/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	defaultDataDir       = ".data"
	defaultBlobBucket    = "syftbox"
	defaultBlobRegion    = "us-east-1"
	defaultBlobEndpoint  = "http://syftboxdev.openmined.org:9000"
	defaultBlobAccessKey = "AbH4qZdboOLES93uUUb2"
	defaultBlobSecretKey = "Pz46w5OYIRO9pAB5urEfyRdSNwLpeQc9CvwguQzX"
)

func main() {
	var certFile string
	var keyFile string
	var addr string
	var dataDir string
	var configFile string

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
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			dataDir, err := utils.ResolvePath(viper.GetString("data_dir"))
			if err != nil {
				return err
			}
			config := &server.Config{
				Http: &server.HttpServerConfig{
					Addr:     addr,
					CertFile: certFile,
					KeyFile:  keyFile,
				},
				Blob: &blob.S3BlobConfig{
					BucketName: viper.GetString("blob.bucket_name"),
					Region:     viper.GetString("blob.region"),
					Endpoint:   viper.GetString("blob.endpoint"),
					AccessKey:  viper.GetString("blob.access_key"),
					SecretKey:  viper.GetString("blob.secret_key"),
				},
				DbPath: filepath.Join(dataDir, "state.db"),
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
				"db.path", config.DbPath,
			)

			c, err := server.New(config)
			if err != nil {
				slog.Error("Failed to create server", "error", err)
				return err
			}

			defer slog.Info("Bye!")
			return c.Start(cmd.Context())
		},
	}

	rootCmd.Flags().StringVarP(&certFile, "cert", "c", "", "Path to the certificate file")
	rootCmd.Flags().StringVarP(&keyFile, "key", "k", "", "Path to the key file")
	rootCmd.Flags().StringVarP(&addr, "bind", "b", server.DefaultAddr, "Address to bind the server")
	rootCmd.Flags().StringVarP(&dataDir, "dataDir", "d", defaultDataDir, "Address to bind the server")
	rootCmd.Flags().StringVarP(&configFile, "config", "f", "", "Path to config file")

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

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	viper.BindPFlag("data_dir", cmd.Flags().Lookup("dataDir"))

	// Set up environment variables
	viper.SetEnvPrefix("SYFTBOX")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("blob.bucket_name", defaultBlobBucket)
	viper.SetDefault("blob.region", defaultBlobRegion)
	viper.SetDefault("blob.endpoint", defaultBlobEndpoint)
	viper.SetDefault("blob.access_key", defaultBlobAccessKey)
	viper.SetDefault("blob.secret_key", defaultBlobSecretKey)

	return nil
}

// maskSecret returns first 4 chars of secret followed by "***"
func maskSecret(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "***"
}
