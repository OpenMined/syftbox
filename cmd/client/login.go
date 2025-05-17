package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newLoginCmd())
}

func newLoginCmd() *cobra.Command {
	var dataDir string
	var serverURL string

	cmd := &cobra.Command{
		Use:     "login",
		Aliases: []string{"init"},
		Short:   "Login to the syftbox datasite",
		Run: func(cmd *cobra.Command, args []string) {
			var authToken *syftsdk.AuthTokenResponse
			var email string

			// fetched from main/rootCmd/persistentFlags
			configPath := cmd.Flag("config").Value.String()

			if cfg, err := getValidConfig(configPath); err == nil {
				fmt.Println(green.Render("**Already logged in**"))
				printConfig(cfg)
				os.Exit(0)
			}

			onEmailSubmit := func(emailInput string) error {
				return syftsdk.VerifyEmail(cmd.Context(), serverURL, emailInput)
			}

			onOTPSubmit := func(emailInput, otpInput string) error {
				token, err := syftsdk.VerifyEmailCode(cmd.Context(), serverURL, &syftsdk.VerifyEmailCodeRequest{
					Email: emailInput,
					Code:  otpInput,
				})
				if err != nil {
					return err
				}
				email = emailInput
				authToken = token

				time.Sleep(500 * time.Millisecond)
				return nil
			}

			if err := RunLoginTUI(onEmailSubmit, onOTPSubmit, utils.IsValidEmail, syftsdk.IsValidOTP); err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			if authToken == nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), "no auth token found")
				os.Exit(1)
			}

			cfg := &config.Config{
				Email:        email,
				DataDir:      dataDir,
				ServerURL:    serverURL,
				ClientURL:    config.DefaultClientURL,
				RefreshToken: authToken.RefreshToken,
				AccessToken:  authToken.AccessToken, // not gonna be serialized
				AppsEnabled:  true,
				Path:         configPath,
			}

			if err := cfg.Validate(); err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			if err := cfg.Save(); err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			fmt.Println(green.Render("SyftBox datasite initialized"))
			printConfig(cfg)
		},
	}

	cmd.Flags().SortFlags = false
	cmd.Flags().StringVarP(&dataDir, "data-dir", "d", defaultDataDir, "data directory")
	cmd.Flags().StringVarP(&serverURL, "server-url", "u", defaultServerURL, "server URL")

	return cmd
}

func getValidConfig(configPath string) (*config.Config, error) {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return nil, err
	}
	return cfg, cfg.Validate()
}

func printConfig(cfg *config.Config) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n%s\n", lightGray.Render("SYFTBOX DATASITE CONFIG")))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Email"), cyan.Render(cfg.Email)))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Data"), cyan.Render(cfg.DataDir)))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Config"), cfg.Path))
	sb.WriteString(fmt.Sprintf("%s\t%s\n", lightGray.Render("Server"), cfg.ServerURL))
	fmt.Println(sb.String())
}
