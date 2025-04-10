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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yashgorana/syftbox-go/internal/client"
	"github.com/yashgorana/syftbox-go/internal/uibridge"
	"github.com/yashgorana/syftbox-go/internal/utils"
	"github.com/yashgorana/syftbox-go/internal/version"
)

var (
	home, _          = os.UserHomeDir()
	defaultDataDir   = filepath.Join(home, "SyftBox")
	defaultServerURL = "https://syftboxdev.openmined.org"
	configFileName   = "config"
)

var rootCmd = &cobra.Command{
	Use:     "syftbox",
	Short:   "SyftBox CLI",
	Version: version.Detailed(),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return loadConfig(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Generate UI token if needed
		uiToken := viper.GetString("ui_token")
		if viper.GetBool("ui") && uiToken == "" {
			uiToken = utils.TokenHex(16)
		}

		// Generate Swagger docs if enabled
		if viper.GetBool("ui") && viper.GetBool("ui_swagger") {
			uibridge.GenerateSwagger()
		}

		c, err := client.New(&client.Config{
			Path:        viper.ConfigFileUsed(),
			Email:       viper.GetString("email"),
			DataDir:     viper.GetString("data_dir"),
			ServerURL:   viper.GetString("server_url"),
			AppsEnabled: viper.GetBool("apps_enabled"),
			UIBridge: uibridge.Config{
				Enabled:        viper.GetBool("ui"),
				Host:           viper.GetString("ui_host"),
				Port:           viper.GetInt("ui_port"),
				Token:          uiToken,
				EnableSwagger:  viper.GetBool("ui_swagger"),
				RequestTimeout: viper.GetDuration("ui_timeout"),
				RateLimit:      viper.GetFloat64("ui_rate_limit"),
				RateLimitBurst: viper.GetInt("ui_rate_burst"),
			},
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
	rootCmd.Flags().StringP("email", "e", "", "Email for the SyftBox datasite")
	rootCmd.Flags().StringP("datadir", "d", defaultDataDir, "SyftBox Data Directory")
	rootCmd.Flags().StringP("server", "s", defaultServerURL, "SyftBox Server")
	rootCmd.PersistentFlags().StringP("config", "c", "", "SyftBox config file")

	// Add UI bridge flags
	rootCmd.Flags().Bool("ui", true, "Enable the UI bridge server")
	rootCmd.Flags().Bool("ui-swagger", false, "Enable Swagger documentation for UI bridge server")
	rootCmd.Flags().String("ui-host", "localhost", "Host to bind the UI bridge server")
	rootCmd.Flags().Int("ui-port", 0, "Port for the UI bridge server (default 0 for random port)")
	rootCmd.Flags().String("ui-token", "", "Access token for the UI bridge server (default random secure token)")
	rootCmd.Flags().Duration("ui-timeout", 30*time.Second, "Request timeout for the UI bridge server")
	rootCmd.Flags().Float64("ui-rate-limit", 10, "Rate limit in requests per second per client IP")
	rootCmd.Flags().Int("ui-rate-burst", 20, "Maximum burst size for rate limiting")

}

func main() {
	handler := tint.NewHandler(os.Stdout, &tint.Options{
		AddSource:  true,
		Level:      slog.LevelDebug,
		TimeFormat: time.RFC3339,
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

	// Bind UI bridge flags
	viper.BindPFlag("ui", cmd.Flags().Lookup("ui"))
	viper.BindPFlag("ui_host", cmd.Flags().Lookup("ui-host"))
	viper.BindPFlag("ui_port", cmd.Flags().Lookup("ui-port"))
	viper.BindPFlag("ui_token", cmd.Flags().Lookup("ui-token"))
	viper.BindPFlag("ui_swagger", cmd.Flags().Lookup("ui-swagger"))
	viper.BindPFlag("ui_timeout", cmd.Flags().Lookup("ui-timeout"))
	viper.BindPFlag("ui_rate_limit", cmd.Flags().Lookup("ui-rate-limit"))
	viper.BindPFlag("ui_rate_burst", cmd.Flags().Lookup("ui-rate-burst"))

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
