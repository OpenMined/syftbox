package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yashgorana/syftbox-go/pkg/client"
)

var (
	home, _          = os.UserHomeDir()
	defaultDataDir   = filepath.Join(home, "SyftBox")
	defaultServerURL = "http://localhost:8080"
	configFileName   = "config"
)

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

	var rootCmd = &cobra.Command{
		Use:   "client",
		Short: "SyftBox Client CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.New(&client.Config{
				Email:     viper.GetString("email"),
				DataDir:   viper.GetString("data_dir"),
				ServerURL: viper.GetString("server_url"),
			})
			if err != nil {
				return err
			}
			defer slog.Info("Bye!")
			return c.Start(cmd.Context())
		},
	}

	rootCmd.Flags().StringP("email", "e", "", "Email for the SyftBox datasite")
	rootCmd.Flags().StringP("datadir", "d", defaultDataDir, "SyftBox Data Directory")
	rootCmd.Flags().StringP("server", "s", defaultServerURL, "SyftBox Server")
	// rootCmd.MarkFlagRequired("email")

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func loadConfig(cmd *cobra.Command) error {
	// config path
	viper.AddConfigPath(".")                                    // First check current directory
	viper.AddConfigPath(filepath.Join(home, ".syftbox"))        // Then check .config/syftbox
	viper.AddConfigPath(filepath.Join(home, ".config/syftbox")) // Then check .config/syftbox
	viper.SetConfigName(configFileName)                         // Name of config file (without extension)
	viper.SetConfigType("json")

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Bind flags to viper
	viper.BindPFlag("email", cmd.Flags().Lookup("email"))
	viper.BindPFlag("data_dir", cmd.Flags().Lookup("datadir"))
	viper.BindPFlag("server_url", cmd.Flags().Lookup("server"))

	// Set up environment variables
	viper.SetEnvPrefix("SYFTBOX")
	viper.AutomaticEnv()

	// Validate email
	if err := validateEmail(viper.GetString("email")); err != nil {
		return err
	}

	return nil
}

func validateEmail(email string) error {
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("invalid email")
	}
	return nil
}
