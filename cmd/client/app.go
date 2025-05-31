package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/spf13/cobra"
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
	var useGit bool

	appCmdInstall := &cobra.Command{
		Use:     "install [URL]",
		Aliases: []string{"i", "add"},
		Short:   "Install a SyftBox app",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			repo := args[0]
			installer, err := getAppManager(cmd)
			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}
			app, err := installer.InstallApp(cmd.Context(), apps.AppInstallOpts{
				URI:    repo,
				Branch: branch,
				Tag:    tag,
				Commit: commit,
				Force:  force,
				UseGit: useGit,
			})

			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			fmt.Printf("Installed '%s' at '%s'\n", cyan.Bold(true).Render(app.Name), green.Bold(true).Render(app.Path))
		},
	}

	appCmdInstall.Flags().SortFlags = false
	appCmdInstall.Flags().StringVarP(&branch, "branch", "", "main", "Branch to install from")
	appCmdInstall.Flags().StringVarP(&tag, "tag", "", "", "Tag of the repo to install from")
	appCmdInstall.Flags().StringVarP(&commit, "commit", "", "", "Commit hash of the repo to install from")
	appCmdInstall.Flags().BoolVarP(&force, "force", "f", false, "Force install")
	appCmdInstall.Flags().BoolVarP(&useGit, "use-git", "g", false, "Use git to install")

	return appCmdInstall
}

func newAppCmdUninstall() *cobra.Command {
	appCmdUninstall := &cobra.Command{
		Use:     "uninstall [APPID or URI]",
		Aliases: []string{"u", "rm"},
		Short:   "Uninstall a SyftBox app",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			uri := args[0]
			manager, err := getAppManager(cmd)
			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}
			appID, err := manager.UninstallApp(uri)
			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}
			fmt.Printf("Uninstalled '%s'\n", green.Bold(true).Render(appID))
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
			manager, err := getAppManager(cmd)
			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			appList, err := manager.ListApps()
			if err != nil {
				fmt.Printf("%s: %s\n", red.Render("ERROR"), err)
				os.Exit(1)
			}

			if len(appList) == 0 {
				fmt.Printf("No apps installed at '%s'\n", cyan.Render(manager.AppsDir))
				os.Exit(0)
			}

			var sb strings.Builder
			for idx, app := range appList {
				if idx > 0 {
					sb.WriteString("\n")
				}
				var src string
				sb.WriteString(fmt.Sprintf("%s%s\n", gray.Render("ID      "), green.Render(app.ID)))
				sb.WriteString(fmt.Sprintf("%s%s\n", gray.Render("Path    "), cyan.Render(app.Path)))
				if app.Source != apps.AppSourceLocalDir {
					if app.Branch != "" {
						src = app.Branch
					} else if app.Tag != "" {
						src = app.Tag
					} else {
						src = app.Commit
					}
				} else {
					src = app.Source
				}

				sb.WriteString(fmt.Sprintf("%s%s (%s)\n", gray.Render("Source  "), app.SourceURI, src))
			}
			fmt.Print(sb.String())
		},
	}
	return appCmdList
}

func getAppManager(cmd *cobra.Command) (*apps.AppManager, error) {
	// fetched from main/rootCmd/persistentFlags
	configPath := cmd.Flag("config").Value.String()

	cfg, err := readValidConfig(configPath, false)
	if err != nil {
		return nil, err
	}

	datasite, err := workspace.NewWorkspace(cfg.DataDir, cfg.Email)
	if err != nil {
		return nil, err
	}

	manager := apps.NewManager(datasite.AppsDir, datasite.MetadataDir)
	return manager, nil
}
