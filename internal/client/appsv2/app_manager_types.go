package appsv2

// AppInstallOpts contains options for repository installation
type AppInstallOpts struct {
	URI    string // Local Path or Git URL of the app
	Branch string // Git branch to install
	Tag    string // Git tag to install
	Commit string // Git commit hash to install
	UseGit bool   // Use git to install
	Force  bool   // Force install
}
