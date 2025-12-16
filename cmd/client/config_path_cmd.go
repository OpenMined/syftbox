package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newConfigPathCmd())
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-path",
		Short: "Print the resolved config file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), resolveConfigPath(cmd))
			return err
		},
	}
}

