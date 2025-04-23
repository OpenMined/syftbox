package main

import (
	"fmt"
	"os"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/spf13/cobra"
)

// NOTE: This is a temporary command to initialize the syftbox datasite.

func init() {
	rootCmd.AddCommand(newInitCmd())
}

func newInitCmd() *cobra.Command {
	var email string
	var dataDir string
	var serverURL string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize syftbox datasite",
		Run: func(cmd *cobra.Command, args []string) {
			if cfg, err := config.LoadClientConfig(config.DefaultConfigPath); err == nil {
				fmt.Println("SyftBox Datasite already initialized")
				fmt.Printf("Config Path: %s\n", green(cfg.Path))
				fmt.Printf("Email:       %s\n", cyan(cfg.Email))
				fmt.Printf("Data Dir:    %s\n", cyan(cfg.DataDir))
				fmt.Printf("Server:      %s\n", cyan(cfg.ServerURL))
				os.Exit(0)
			}

			if email == "" {
				fmt.Printf("%s: %s\n", red("ERROR"), "email is required")
				os.Exit(1)
			}

			if dataDir == "" {
				fmt.Printf("%s: %s\n", red("ERROR"), "data-dir is required")
				os.Exit(1)
			}

			if serverURL == "" {
				fmt.Printf("%s: %s\n", red("ERROR"), "server-url is required")
				os.Exit(1)
			}

			cfg := &config.Config{
				Email:       email,
				DataDir:     dataDir,
				ServerURL:   serverURL,
				ClientURL:   "http://localhost:8080",
				AppsEnabled: true,
			}

			if err := cfg.Save(config.DefaultConfigPath); err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}

			fmt.Println("SyftBox Datasite initialized")
			fmt.Printf("Config Path: %s\n", green(cfg.Path))
			fmt.Printf("Email:       %s\n", cyan(cfg.Email))
			fmt.Printf("Data Dir:    %s\n", cyan(cfg.DataDir))
			fmt.Printf("Server:      %s\n", cyan(cfg.ServerURL))
		},
	}

	cmd.Flags().SortFlags = false
	cmd.Flags().StringVarP(&email, "email", "e", "", "email address")
	cmd.Flags().StringVarP(&dataDir, "data-dir", "d", defaultDataDir, "data directory")
	cmd.Flags().StringVarP(&serverURL, "server-url", "u", defaultServerURL, "server URL")

	return cmd
}
