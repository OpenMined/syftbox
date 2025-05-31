package handlers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openmined/syftbox/internal/client/apps"
)

// AppInstallRequest represents the request to install an app.
type AppInstallRequest struct {
	RepoURL string `json:"repoURL" binding:"required"` // url of the github repo to install
	Branch  string `json:"branch"`                     // branch of the repo to install
	Tag     string `json:"tag"`                        // tag of the repo to install
	Commit  string `json:"commit"`                     // commit of the repo to install
	Force   bool   `json:"force"`                      // force install
}

type AppResponse struct {
	// Unique ID of the app [deprecated]
	ID string `json:"id"`
	// name of the app [deprecated]
	Name string `json:"name"`
	// Absolute path to the app from the workspace root [deprecated]
	Path string `json:"path"`
	// Info about the app
	Info *apps.AppInfo `json:"info"`
	// Status of the app
	Status apps.AppProcessStatus `json:"status"`
	// Process ID of the app's run.sh
	PID int32 `json:"pid,omitempty"`
	// List of ports this app is listening on
	Ports []uint32 `json:"ports,omitempty"`
	// Extended process statistics (optional)
	ProcessStats *apps.ProcessStats `json:"processStats,omitempty"`
}

func NewAppResponse(app *apps.App, processStats bool) (*AppResponse, error) {
	appInfo := app.Info()
	appState := app.GetStatus()
	appProc := app.Process()

	// if not running, return a basic response
	if appState != apps.StatusRunning {
		return &AppResponse{
			Info:   appInfo,
			ID:     appInfo.ID,
			Name:   appInfo.ID,
			Path:   appInfo.Path,
			Status: appState,
		}, nil
	}

	// if running, return a full response
	appResp := &AppResponse{
		Info:   appInfo,
		ID:     appInfo.ID,
		Name:   appInfo.ID,
		Path:   appInfo.Path,
		Status: appState,
		PID:    appProc.Pid,
		Ports:  apps.ProcessListenPorts(appProc),
	}

	if processStats {
		stats, err := apps.NewProcessStats(appProc)
		if err != nil {
			return nil, fmt.Errorf("failed to get process stats: %w", err)
		} else {
			appResp.ProcessStats = stats
		}
	}

	return appResp, nil
}

// MarshalJSON implements json.Marshaler interface for AppResponse
func (a AppResponse) MarshalJSON() ([]byte, error) {
	type Alias AppResponse
	return json.Marshal(&struct {
		Path string `json:"path"`
		*Alias
	}{
		Path:  strings.ReplaceAll(a.Path, "\\", "/"),
		Alias: (*Alias)(&a),
	})
}

// UnmarshalJSON implements json.Unmarshaler interface for AppResponse
func (a *AppResponse) UnmarshalJSON(data []byte) error {
	type Alias AppResponse
	aux := &struct {
		Path string `json:"path"`
		*Alias
	}{
		Alias: (*Alias)(a),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	a.Path = strings.ReplaceAll(aux.Path, "\\", "/")
	return nil
}

// AppListResponse represents the response to list all installed apps.
type AppListResponse struct {
	Apps []*AppResponse `json:"apps"` // list of installed apps
}
