package handlers

// AppInstallRequest represents the request to install an app.
type AppInstallRequest struct {
	RepoURL string `json:"repoURL" binding:"required"` // url of the github repo to install
	Branch  string `json:"branch"`                     // branch of the repo to install
	Tag     string `json:"tag"`                        // tag of the repo to install
	Commit  string `json:"commit"`                     // commit of the repo to install
	Force   bool   `json:"force"`                      // force install
}

// AppUninstallRequest represents the request to uninstall an app.
type AppUninstallRequest struct {
	AppName string `form:"appName" binding:"required"` // name of the app to uninstall
}

// AppListResponse represents the response to list all installed apps.
type AppListResponse struct {
	Apps []string `json:"apps"` // list of installed apps
}
