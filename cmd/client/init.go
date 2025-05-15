package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
			// fetched from main/rootCmd/persistentFlags
			configPath := viper.GetString("config")

			if cfg, err := config.LoadFromFile(configPath); err == nil {
				fmt.Println("SyftBox Datasite already initialized")
				fmt.Printf("Config Path: %s\n", green(cfg.Path))
				fmt.Printf("Email:       %s\n", cyan(cfg.Email))
				fmt.Printf("Data Dir:    %s\n", cyan(cfg.DataDir))
				fmt.Printf("Server:      %s\n", cyan(cfg.ServerURL))
				os.Exit(0)
			}

			if dataDir == "" {
				fmt.Printf("%s: %s\n", red("ERROR"), "data-dir is required")
				os.Exit(1)
			}

			if serverURL == "" {
				fmt.Printf("%s: %s\n", red("ERROR"), "server-url is required")
				os.Exit(1)
			}

			if email == "" {
				fmt.Printf("Enter your email: ")
				fmt.Scanln(&email)
			}

			if err := utils.ValidateEmail(email); err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}

			authToken, err := doLogin(cmd.Context(), serverURL, email)
			if err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}

			cfg := &config.Config{
				Email:       email,
				DataDir:     dataDir,
				ServerURL:   serverURL,
				ClientURL:   "http://localhost:8080",
				AccessToken: authToken.AccessToken,
				AppsEnabled: true,
				Path:        configPath,
			}

			if err := cfg.Save(); err != nil {
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

func doLogin(ctx context.Context, serverURL string, email string) (*syftsdk.AuthTokenResponse, error) {
	if err := syftsdk.VerifyEmail(ctx, serverURL, email); err != nil {
		return nil, err
	}

	// prompt for the OTP code
	fmt.Printf("Enter the OTP code sent to %s: ", email)
	var emailCode string
	fmt.Scanln(&emailCode)

	authToken, err := syftsdk.VerifyEmailCode(ctx, serverURL, &syftsdk.VerifyEmailCodeRequest{
		Email: email,
		Code:  emailCode,
	})

	if err != nil {
		return nil, err
	}

	return authToken, nil
}
