package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/openmined/syftbox/internal/client"
	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/openmined/syftbox/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	home, _          = os.UserHomeDir()
	defaultDataDir   = filepath.Join(home, "SyftBox")
	defaultServerURL = "https://syftboxdev.openmined.org"
	configFileName   = "config"
)

var (
	red   = color.New(color.FgHiRed, color.Bold).SprintFunc()
	green = color.New(color.FgHiGreen).SprintFunc()
	cyan  = color.New(color.FgHiCyan).SprintFunc()
)

var rootCmd = &cobra.Command{
	Use:     "syftbox",
	Short:   "SyftBox CLI",
	Version: version.Detailed(),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return loadConfig(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		c, err := client.New(&config.Config{
			Path:         viper.ConfigFileUsed(),
			Email:        viper.GetString("email"),
			DataDir:      viper.GetString("data_dir"),
			ServerURL:    viper.GetString("server_url"),
			RefreshToken: viper.GetString("refresh_token"),
			AppsEnabled:  viper.GetBool("apps_enabled"),
			AccessToken:  viper.GetString("access_token"),
			ClientURL:    "http://localhost:8080", // dummy value to make sure apps dont break
		})
		if err != nil {
			return err
		}
		defer slog.Info("Bye!")
		showSyftBoxHeader()
		return c.Start(cmd.Context())
	},
}

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.Flags().StringP("email", "e", "", "Email for the SyftBox datasite")
	rootCmd.Flags().StringP("datadir", "d", defaultDataDir, "SyftBox Data Directory")
	rootCmd.Flags().StringP("server", "s", defaultServerURL, "SyftBox Server")
	rootCmd.PersistentFlags().StringP("config", "c", "", "SyftBox config file")
}

func main() {
	// TODO handle log rotation
	// TODO unique log file for each instance to handle multiple daemons
	logFile := config.DefaultLogFilePath

	logDir := filepath.Dir(logFile)
	// Create log directory
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create log directory: %v\n", err)
		os.Exit(1)
	}

	// Create new log file for this instance
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Setup handlers for both outputs
	stdoutHandler := tint.NewHandler(os.Stdout, &tint.Options{
		AddSource:  true,
		Level:      slog.LevelDebug,
		TimeFormat: time.RFC3339Nano,
		NoColor:    !isatty.IsTerminal(os.Stdout.Fd()),
	})
	logInterceptor := utils.NewLogInterceptor(file)
	fileHandler := slog.NewTextHandler(logInterceptor, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		// Do not include time as it is added by the log interceptor.
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{} // Remove the time attribute
			}
			return a
		},
	})

	// Create multi-handler
	multiLogHandler := utils.NewMultiLogHandler(stdoutHandler, fileHandler)
	logger := slog.New(multiLogHandler)
	slog.SetDefault(logger)

	// Setup root context with signal handling
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func loadConfig(cmd *cobra.Command) error {
	// config path
	if cmd.Flag("config").Changed {
		configFilePath, _ := cmd.Flags().GetString("config")
		viper.SetConfigFile(configFilePath)
	} else {
		viper.AddConfigPath(filepath.Join(home, ".syftbox"))        // Then check .syftbox
		viper.AddConfigPath(filepath.Join(home, ".config/syftbox")) // Then check .config/syftbox
		viper.SetConfigName(configFileName)                         // Name of config file (without extension)
		viper.SetConfigType("json")
	}

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

	// env only
	viper.SetDefault("apps_enabled", true)

	// Set up environment variables
	viper.SetEnvPrefix("SYFTBOX")
	viper.AutomaticEnv()

	// Validate email
	if err := validateEmail(viper.GetString("email")); err != nil {
		return err
	}

	// override server url if remote url is set
	if strings.Contains(viper.GetString("server_url"), "openmined.org") {
		viper.Set("server_url", defaultServerURL)
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

func showSyftBoxHeader() {
	color.New(color.FgHiCyan, color.Bold).
		Print(utils.SyftBoxArt + "\n")
}
