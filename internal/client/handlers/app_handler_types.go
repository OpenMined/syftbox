package handlers

import (
	"encoding/json"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

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
	Ports []uint32 `json:"ports"`
	// Extended process statistics (optional)
	ProcessStats *ProcessStats `json:"processStats,omitempty"`
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
	Apps []AppResponse `json:"apps"` // list of installed apps
}

type ProcessStats struct {
	// Process Name
	ProcessName string `json:"processName"`
	// Process ID
	PID int32 `json:"pid"`
	// Status of the process
	Status []string `json:"status"`
	// Command line arguments for this app's process
	Cmdline []string `json:"cmdline"`
	// Current working directory of this app's process
	CWD string `json:"cwd"`
	// Environment variables for this app's process
	Environ []string `json:"environ"`
	// Executable path of this app's process
	Exe string `json:"exe"`
	// List of groups this app is a member of
	Gids []uint32 `json:"gids"`
	// List of user IDs this app is a member of
	Uids []uint32 `json:"uids"`
	// Nice value of this app's process
	Nice int32 `json:"nice"`
	// Username of the user this app is running as
	Username string `json:"username"`
	// All connections this app is listening on
	Connections []psnet.ConnectionStat `json:"connections"`
	// Percentage of total CPU this app is using
	CPUPercent float64 `json:"cpuPercent"`
	// CPU times breakdown
	CPUTimes *cpu.TimesStat `json:"cpuTimes"`
	// Number of threads this app is using
	NumThreads int32 `json:"numThreads"`
	// Percentage of total RAM this app is using
	MemoryPercent float32 `json:"memoryPercent"`
	// Memory info
	MemoryInfo *process.MemoryInfoStat `json:"memoryInfo"`
	// How long the app has been running in milliseconds
	Uptime int64 `json:"uptime"`
	// Children processes
	Children []ProcessStats `json:"children"`
}
