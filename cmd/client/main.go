package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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
		// create & validate config
		cfg := &config.Config{
			Path:         viper.ConfigFileUsed(),
			Email:        viper.GetString("email"),
			DataDir:      viper.GetString("data_dir"),
			ServerURL:    viper.GetString("server_url"),
			RefreshToken: viper.GetString("refresh_token"),
			AppsEnabled:  viper.GetBool("apps_enabled"),
			ClientURL:    "http://localhost:8080", // dummy value to make sure apps dont break
		}
		if err := cfg.Validate(); err != nil {
			return err
		}

		// all good now, show header
		cmd.SilenceUsage = true
		showSyftBoxHeader()

		// create client
		c, err := client.New(cfg)
		if err != nil {
			return err
		}

		// start client
		defer slog.Info("Bye!")
		return c.Start(cmd.Context())
	},
}

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.Flags().StringP("email", "e", "", "Email for the SyftBox datasite")
	rootCmd.Flags().StringP("datadir", "d", defaultDataDir, "SyftBox Data Directory")
	rootCmd.Flags().StringP("server", "s", defaultServerURL, "SyftBox Server")
	rootCmd.PersistentFlags().StringP("config", "c", config.DefaultConfigPath, "SyftBox config file")
}

func main() {
	handler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: "2006-01-02T15:04:05.000Z07:00",
		NoColor:    !isatty.IsTerminal(os.Stdout.Fd()),
	})
	logger := slog.New(handler)
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
		enoent := errors.Is(err, os.ErrNotExist)
		_, ok := err.(viper.ConfigFileNotFoundError)
		if !enoent && !ok {
			return fmt.Errorf("config read '%s': %w", viper.ConfigFileUsed(), err)
		}
	}

	// Bind flags to viper
	viper.BindPFlag("email", cmd.Flags().Lookup("email"))
	viper.BindPFlag("data_dir", cmd.Flags().Lookup("datadir"))
	viper.BindPFlag("server_url", cmd.Flags().Lookup("server"))

	// Set up environment variables
	viper.SetEnvPrefix("SYFTBOX")
	viper.AutomaticEnv()

	// override server url if remote url is set
	if strings.Contains(viper.GetString("server_url"), "openmined.org") {
		viper.Set("server_url", defaultServerURL)
	}

	return nil
}

func showSyftBoxHeader() {
	color.New(color.FgHiCyan, color.Bold).
		Print(utils.SyftBoxArt + "\n")
}
