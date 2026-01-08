package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/spf13/cobra"
)

var watchStatusCmd = &cobra.Command{
	Use:   "watch-status",
	Short: "Continuously poll local control plane /v1/status",
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, _ := cmd.Flags().GetDuration("interval")
		raw, _ := cmd.Flags().GetBool("raw")

		cfg, err := loadConfig(cmd)
		if err != nil {
			return err
		}
		if envURL := os.Getenv("SYFTBOX_CLIENT_URL"); envURL != "" {
			cfg.ClientURL = envURL
		}
		if envToken := os.Getenv("SYFTBOX_CLIENT_TOKEN"); envToken != "" {
			cfg.ClientToken = envToken
		}
		if cfg.ClientURL == "" || cfg.ClientToken == "" {
			return fmt.Errorf("client control plane not configured; set --client-url/--client-token or SYFTBOX_CLIENT_URL/SYFTBOX_CLIENT_TOKEN")
		}

		statusURL := fmt.Sprintf("%s/v1/status", cfg.ClientURL)
		client := &http.Client{Timeout: 5 * time.Second}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-cmd.Context().Done():
				return nil
			case <-ticker.C:
				req, _ := http.NewRequestWithContext(cmd.Context(), http.MethodGet, statusURL, nil)
				req.Header.Set("Authorization", "Bearer "+cfg.ClientToken)
				resp, err := client.Do(req)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s ERROR %v\n", time.Now().UTC().Format(time.RFC3339), err)
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if raw {
					fmt.Printf("%s\n", body)
					continue
				}

				var v any
				if err := json.Unmarshal(body, &v); err != nil {
					fmt.Printf("%s\n", body)
					continue
				}
				pretty, _ := json.MarshalIndent(v, "", "  ")
				fmt.Printf("%s\n", pretty)
			}
		}
	},
}

func init() {
	watchStatusCmd.Flags().Duration("interval", 1*time.Second, "poll interval")
	watchStatusCmd.Flags().Bool("raw", false, "print raw json without pretty formatting")

	// Inherit global flags for client-url/client-token.
	rootCmd.AddCommand(watchStatusCmd)

	// Ensure defaults match config package for help text.
	_ = config.DefaultClientURL
}
