package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/openmined/syftbox/internal/server"
	"github.com/openmined/syftbox/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	DefaultBindAddr           = "localhost:8080"
	DefaultDataDir            = ".data"
	DefaultAuthEnabled        = false
	DefaultEmailOTPLength     = 8
	DefaultEmailOTPExpiry     = 5 * time.Minute
	DefaultRefreshTokenExpiry = 0
	DefaultAccessTokenExpiry  = 7 * 24 * time.Hour
)

var dotenvLoaded bool

var rootCmd = &cobra.Command{
	Use:     "server",
	Short:   "SyftBox Server CLI",
	Version: version.Detailed(),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		// load config
		cfg, err := loadConfig(cmd)
		if err != nil {
			cmd.SilenceUsage = false // show usage
			return err
		}

		// Log the final configuration details (masking secrets)
		logConfig(cfg)

		c, err := server.New(cfg)
		if err != nil {
			slog.Error("Failed to create server", "error", err)
			return err
		}

		defer slog.Info("Bye!")
		if err := c.Start(cmd.Context()); err != nil {
			slog.Error("Failed to start server", "error", err)
			return err
		}
		return nil
	},
}

func init() {
	// Only setup server-related CLI flags
	rootCmd.Flags().SortFlags = false
	rootCmd.Flags().StringP("config", "f", "", "Path to config file (e.g., config.yaml)")
	rootCmd.Flags().StringP("bind", "b", DefaultBindAddr, "Address to bind the server")
	rootCmd.Flags().StringP("cert", "c", "", "Path to the certificate file for HTTPS")
	rootCmd.Flags().StringP("key", "k", "", "Path to the key file for HTTPS")
	rootCmd.Flags().StringP("dataDir", "d", DefaultDataDir, "Directory for server data")

	if err := godotenv.Load(".env"); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Println("Error loading .env file", err)
			os.Exit(1)
		}
	} else {
		dotenvLoaded = true
	}
}

func main() {
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

	// server go brr
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

// loadConfig initializes viper, reads config file/env vars, and maps values to config
func loadConfig(cmd *cobra.Command) (*server.Config, error) {
	v := viper.New()

	// Set up config file
	if cmd.Flag("config").Changed {
		configFilePath := cmd.Flag("config").Value.String()
		v.SetConfigFile(configFilePath)
		slog.Info("Using config file specified via flag", "path", configFilePath)
	} else {
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.syftbox")
		v.AddConfigPath("/etc/syftbox/")
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.SetConfigType("json")
	}

	// Set up environment variables
	v.SetEnvPrefix("SYFTBOX")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	bindWithDefaults(v, cmd)

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file '%s': %w", v.ConfigFileUsed(), err)
		}
	} else {
		slog.Debug("Loaded configuration from file", "path", v.ConfigFileUsed())
	}

	// Unmarshal to server.Config
	var cfg *server.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshalling config: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func bindWithDefaults(v *viper.Viper, cmd *cobra.Command) {
	// Bind CLI flags to viper
	v.BindPFlag("http.addr", cmd.Flags().Lookup("bind"))
	v.BindPFlag("http.cert_file", cmd.Flags().Lookup("cert"))
	v.BindPFlag("http.key_file", cmd.Flags().Lookup("key"))
	v.BindPFlag("data_dir", cmd.Flags().Lookup("dataDir"))

	// Set default values. REQUIRED.

	// Data directory
	v.SetDefault("data_dir", DefaultDataDir)
	// HTTP section
	v.SetDefault("http.addr", DefaultBindAddr)
	v.SetDefault("http.cert_file", "")
	v.SetDefault("http.key_file", "")
	// Blob section (config file/env vars only)
	v.SetDefault("blob.bucket_name", "")
	v.SetDefault("blob.region", "")
	v.SetDefault("blob.endpoint", "")
	v.SetDefault("blob.access_key", "")
	v.SetDefault("blob.secret_key", "")
	// Auth section (config file/env vars only)
	v.SetDefault("auth.enabled", DefaultAuthEnabled)
	v.SetDefault("auth.token_issuer", "")
	v.SetDefault("auth.email_otp_length", DefaultEmailOTPLength)
	v.SetDefault("auth.email_otp_expiry", DefaultEmailOTPExpiry)
	v.SetDefault("auth.refresh_token_secret", "")
	v.SetDefault("auth.refresh_token_expiry", DefaultRefreshTokenExpiry)
	v.SetDefault("auth.access_token_secret", "")
	v.SetDefault("auth.access_token_expiry", DefaultAccessTokenExpiry)

}

func logConfig(cfg *server.Config) {
	slog.Info("server config",
		"dotenv", dotenvLoaded,
		"http.addr", cfg.HTTP.Addr,
		"http.cert_file", cfg.HTTP.CertFile,
		"http.key_file", cfg.HTTP.KeyFile,
		"data_dir", cfg.DataDir,
		"blob.bucket_name", cfg.Blob.BucketName,
		"blob.region", cfg.Blob.Region,
		"blob.endpoint", cfg.Blob.Endpoint,
		"blob.access_key", maskSecret(cfg.Blob.AccessKey),
		"blob.secret_key", maskSecret(cfg.Blob.SecretKey),
		"auth.enabled", cfg.Auth.Enabled,
		"auth.token_issuer", cfg.Auth.TokenIssuer,
		"auth.email_otp_length", cfg.Auth.EmailOTPLength,
		"auth.email_otp_expiry", cfg.Auth.EmailOTPExpiry,
		"auth.refresh_token_secret", maskSecret(cfg.Auth.RefreshTokenSecret),
		"auth.refresh_token_expiry", cfg.Auth.RefreshTokenExpiry,
		"auth.access_token_secret", maskSecret(cfg.Auth.AccessTokenSecret),
		"auth.access_token_expiry", cfg.Auth.AccessTokenExpiry,
	)
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "***"
}
