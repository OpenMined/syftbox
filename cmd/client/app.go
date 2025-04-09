package main

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yashgorana/syftbox-go/internal/client/apps"
	"github.com/yashgorana/syftbox-go/internal/client/datasite"
)

var (
	red   = color.New(color.FgHiRed, color.Bold).SprintFunc()
	green = color.New(color.FgHiGreen).SprintFunc()
	cyan  = color.New(color.FgHiCyan).SprintFunc()
)

func init() {
	appCmd := newAppCmd()
	appCmd.AddCommand(newAppCmdList())
	appCmd.AddCommand(newAppCmdInstall())
	appCmd.AddCommand(newAppCmdUninstall())
	rootCmd.AddCommand(appCmd)
}

func newAppCmd() *cobra.Command {
	appCmd := &cobra.Command{
		Use:   "app",
		Short: "Manage SyftBox apps",
	}
	return appCmd
}

func newAppCmdInstall() *cobra.Command {
	var branch string
	var tag string
	var commit string
	var force bool

	appCmdInstall := &cobra.Command{
		Use:     "install [URL]",
		Aliases: []string{"i", "add"},
		Short:   "Install a SyftBox app",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			repo := args[0]
			installer, err := getAppManager()
			if err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}
			app, err := installer.InstallRepo(repo, &apps.RepoOpts{
				Branch: branch,
				Tag:    tag,
				Commit: commit,
			}, force)

			if err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}

			fmt.Printf("Installed app '%s' at '%s'\n", green(app.Name), cyan(app.Path))
		},
	}

	appCmdInstall.Flags().SortFlags = false
	appCmdInstall.Flags().StringVarP(&branch, "branch", "b", "main", "Branch to install from")
	appCmdInstall.Flags().StringVarP(&tag, "tag", "t", "", "Tag of the repo to install from")
	appCmdInstall.Flags().StringVarP(&commit, "hash", "s", "", "Commit hash of the repo to install from")
	appCmdInstall.Flags().BoolVarP(&force, "force", "", false, "Force install")

	return appCmdInstall
}

func newAppCmdUninstall() *cobra.Command {
	appCmdUninstall := &cobra.Command{
		Use:     "uninstall [APP_NAME]",
		Aliases: []string{"u", "rm"},
		Short:   "Uninstall a SyftBox app",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]
			manager, err := getAppManager()
			if err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}
			err = manager.UninstallApp(appName)
			if err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}
			fmt.Printf("Uninstalled app '%s'\n", green(appName))
		},
	}

	return appCmdUninstall
}

func newAppCmdList() *cobra.Command {
	appCmdList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List SyftBox apps",
		Run: func(cmd *cobra.Command, args []string) {
			manager, err := getAppManager()
			if err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}

			apps, err := manager.ListApps()
			if err != nil {
				fmt.Printf("%s: %s\n", red("ERROR"), err)
				os.Exit(1)
			}

			if len(apps) == 0 {
				fmt.Printf("No SyftBox Apps installed at '%s'\n", cyan(manager.AppsDir))
				os.Exit(0)
			}

			fmt.Printf("SyftBox Apps at '%s'\n", cyan(manager.AppsDir))
			for _, app := range apps {
				fmt.Printf("- %s\n", green(app))
			}
		},
	}
	return appCmdList
}

func getAppManager() (*apps.Manager, error) {
	user := viper.GetString("user")
	dataDir := viper.GetString("data_dir")

	datasite, err := datasite.NewLocalDatasite(dataDir, user)
	if err != nil {
		return nil, err
	}

	installer := apps.NewManager(datasite.AppsDir)
	return installer, nil
}
