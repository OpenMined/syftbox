package main

import (
	"fmt"
	"os"
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
	var quiet bool

	cmd := &cobra.Command{
		Use:     "login",
		Aliases: []string{"init"},
		Short:   "Login to the syftbox datasite",
		Run: func(cmd *cobra.Command, args []string) {
			var authToken *syftsdk.AuthTokenResponse
			var email string

			// fetched from main/rootCmd/persistentFlags
			configPath := cmd.Flag("config").Value.String()

			if cfg, err := readValidConfig(configPath, true); err == nil {
				if !quiet {
					fmt.Println(green.Render("**Already logged in**"))
					logConfig(cfg)
				}
				os.Exit(0)
			}

			if err := utils.ValidateURL(serverURL); err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
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

			resolvedDataDir, err := utils.ResolvePath(dataDir)
			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			resolvedConfigPath, err := utils.ResolvePath(configPath)
			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			if err := RunLoginTUI(LoginTUIOpts{
				Email:              email,
				ServerURL:          serverURL,
				DataDir:            resolvedDataDir,
				ConfigPath:         resolvedConfigPath,
				EmailSubmitHandler: onEmailSubmit,
				OTPSubmitHandler:   onOTPSubmit,
				EmailValidator:     utils.IsValidEmail,
				OTPValidator:       syftsdk.IsValidOTP,
			}); err != nil {
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

			if !quiet {
				fmt.Println(green.Render("SyftBox datasite initialized"))
				logConfig(cfg)
			}
		},
	}

	cmd.Flags().SortFlags = false
	cmd.Flags().StringVarP(&dataDir, "datadir", "d", config.DefaultDataDir, "data directory where the syftbox workspace is stored")
	cmd.Flags().StringVarP(&serverURL, "server", "s", config.DefaultServerURL, "url of the syftbox server")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "disable output")

	return cmd
}
