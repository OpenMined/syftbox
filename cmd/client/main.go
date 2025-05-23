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
	home, _ = os.UserHomeDir()
)

var rootCmd = &cobra.Command{
	Use:     "syftbox",
	Short:   "SyftBox CLI",
	Version: version.Detailed(),
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", red.Bold(true).Render("ERROR"), err)
			os.Exit(1)
		}

		if err := cfg.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "%s: syftbox config: %s\n", red.Bold(true).Render("ERROR"), err)
			if cfg.Email == "" || cfg.DataDir == "" || cfg.RefreshToken == "" {
				fmt.Fprintf(os.Stderr, "Have you logged in? Run `%s` to fix this\n", green.Render("syftbox login"))
			}
			os.Exit(1)
		}

		// all good now, show header
		cmd.SilenceUsage = true
		showSyftBoxHeader()
		slog.Info("syftbox", "version", version.Version, "revision", version.Revision, "build", version.BuildDate)

		// create client
		c, err := client.New(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: starting client: %s\n", red.Bold(true).Render("ERROR"), err)
			os.Exit(1)
		}

		// start client
		defer slog.Info("Bye!")

		if err := c.Start(cmd.Context()); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "%s: syftbox client: %s\n", red.Bold(true).Render("ERROR"), err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.Flags().StringP("email", "e", "", "your email for your syftbox datasite")
	rootCmd.Flags().StringP("datadir", "d", config.DefaultDataDir, "data directory where the syftbox workspace is stored")
	rootCmd.Flags().StringP("server", "s", config.DefaultServerURL, "url of the syftbox server")
	rootCmd.PersistentFlags().StringP("config", "c", config.DefaultConfigPath, "path to config file")
}

func main() {
	// TODO handle log rotation
	// TODO unique log file for each instance to handle multiple daemons
	logFile := config.DefaultLogFilePath

	logDir := filepath.Dir(logFile)
	// Create log directory
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s - %s\n", red.Bold(true).Render("ERROR"), "create log directory", err)
		os.Exit(1)
	}

	// Create new log file for this instance
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s - %s\n", red.Bold(true).Render("ERROR"), "open log file", err)
		os.Exit(1)
	}
	defer file.Close()

	// Setup handlers for both outputs
	stdoutHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: "2006-01-02T15:04:05.000Z07:00",
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

// loadConfig initializes a config by reading config file/env vars, and cli flags
// it does not guarantee if the contents are valid, as validation is delegated to the client
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	v := viper.New()

	// config path
	if cmd.Flag("config").Changed {
		configFilePath := cmd.Flag("config").Value.String()
		v.SetConfigFile(configFilePath)
	} else {
		v.AddConfigPath(filepath.Join(home, ".syftbox"))        // Then check .syftbox
		v.AddConfigPath(filepath.Join(home, ".config/syftbox")) // Then check .config/syftbox
		v.SetConfigName("config")                               // Name of config file (without extension)
		v.SetConfigType("json")
	}

	// Set up environment variables
	v.SetEnvPrefix("SYFTBOX")
	v.AutomaticEnv()

	// Bind cmd flags to viper & set defaults
	bindWithDefaults(v, cmd)

	// Read config filew
	if err := v.ReadInConfig(); err != nil {
		enoent := errors.Is(err, os.ErrNotExist)
		_, ok := err.(viper.ConfigFileNotFoundError)
		if !enoent && !ok {
			return nil, fmt.Errorf("config read '%s': %w", v.ConfigFileUsed(), err)
		}
	}

	// Unmarshal to server.Config
	var cfg *config.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config read: %w", err)
	}

	return cfg, nil
}

func bindWithDefaults(v *viper.Viper, cmd *cobra.Command) {
	v.BindPFlag("email", cmd.Flags().Lookup("email"))
	v.BindPFlag("data_dir", cmd.Flags().Lookup("datadir"))
	v.BindPFlag("server_url", cmd.Flags().Lookup("server"))
	v.BindPFlag("config_path", cmd.PersistentFlags().Lookup("config"))
	v.SetDefault("client_url", config.DefaultClientURL) // this is not used in standard mode
	v.SetDefault("apps_enabled", config.DefaultAppsEnabled)
	v.SetDefault("refresh_token", "")
	v.SetDefault("access_token", "")
}

// readValidConfig loads a valid config file at a path
// does not rely on viper or cobra
func readValidConfig(configPath string) (*config.Config, error) {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token found")
	}
	return cfg, nil
}

func logConfig(cfg *config.Config) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n%s\n", lightGray.Render("SYFTBOX DATASITE CONFIG")))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Email"), cyan.Render(cfg.Email)))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Data"), cyan.Render(cfg.DataDir)))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Config"), cfg.Path))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Server"), cfg.ServerURL))
	fmt.Println(sb.String())
}

func showSyftBoxHeader() {
	fmt.Println(cyan.Render(utils.SyftBoxArt))
}
