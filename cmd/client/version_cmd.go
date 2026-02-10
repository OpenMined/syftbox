package main

import (
	"fmt"

	"github.com/openmined/syftbox/internal/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newVersionCmd())
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print SyftBox version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Detailed())
			return err
		},
	}
}

