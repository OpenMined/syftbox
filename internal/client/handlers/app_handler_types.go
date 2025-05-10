package handlers

// AppInstallRequest represents the request to install an app.
type AppInstallRequest struct {
	RepoURL string `json:"repoURL" binding:"required"` // url of the github repo to install
	Branch  string `json:"branch"`                     // branch of the repo to install
	Tag     string `json:"tag"`                        // tag of the repo to install
	Commit  string `json:"commit"`                     // commit of the repo to install
	Force   bool   `json:"force"`                      // force install
}

type AppStatus string

const (
	AppStatusRunning AppStatus = "running"
	AppStatusStopped AppStatus = "stopped"
)

type AppResponse struct {
	// Unique name of the app
	Name string `json:"name"`
	// Absolute path to the app from the workspace root
	Path string `json:"path"`
	// Status of the app
	Status AppStatus `json:"status"`
	// Process ID of the app's run.sh
	PID int32 `json:"pid"`
	// List of ports this app is listening on
	Ports []int64 `json:"ports"`
	// Percentage of CPU this app is using
	CPU float64 `json:"cpu"`
	// Percentage of total RAM this app is using
	Memory float32 `json:"memory"`
	// How long the app has been running in milliseconds
	Uptime int64 `json:"uptime"`
}

// AppListResponse represents the response to list all installed apps.
type AppListResponse struct {
	Apps []AppResponse `json:"apps"` // list of installed apps
}
