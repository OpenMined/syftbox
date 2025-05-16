package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/client/datasite"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	ErrCodeListFailed      = "ERR_LIST_FAILED"
	ErrCodeGetFailed       = "ERR_GET_FAILED"
	ErrCodeInstallFailed   = "ERR_INSTALL_FAILED"
	ErrCodeUninstallFailed = "ERR_UNINSTALL_FAILED"
)

type AppHandler struct {
	mgr *datasitemgr.DatasiteManager
}

func NewAppHandler(mgr *datasitemgr.DatasiteManager) *AppHandler {
	return &AppHandler{
		mgr: mgr,
	}
}

// List all installed apps
//
//	@Summary		List apps
//	@Description	List all installed apps
//	@Tags			Apps
//	@Produce		json
//	@Success		200	{object}	AppListResponse
//	@Failure		400	{object}	ControlPlaneError
//	@Failure		401	{object}	ControlPlaneError
//	@Failure		403	{object}	ControlPlaneError
//	@Failure		409	{object}	ControlPlaneError
//	@Failure		429	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Failure		503	{object}	ControlPlaneError
//	@Router			/v1/apps/ [get]
func (h *AppHandler) List(c *gin.Context) {
	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	appNames, err := ds.GetAppManager().ListApps()
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeListFailed,
			Error:     err.Error(),
		})
		return
	}

	apps := enrichApps(ds, appNames)

	c.PureJSON(http.StatusOK, &AppListResponse{
		Apps: apps,
	})
}

// Get an app
//
//	@Summary		Get app
//	@Description	Get an app
//	@Tags			Apps
//	@Produce		json
//	@Param			appName			path		string	true	"App name"
//	@Param			processStats	query		bool	false	"Whether to include process statistics"
//	@Success		200				{object}	AppResponse
//	@Failure		400				{object}	ControlPlaneError
//	@Failure		401				{object}	ControlPlaneError
//	@Failure		403				{object}	ControlPlaneError
//	@Failure		409				{object}	ControlPlaneError
//	@Failure		429				{object}	ControlPlaneError
//	@Failure		500				{object}	ControlPlaneError
//	@Failure		503				{object}	ControlPlaneError
//	@Router			/v1/apps/{appName} [get]
func (h *AppHandler) Get(c *gin.Context) {
	appName := c.Param("appName")
	processStats := c.Query("processStats") == "true"

	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	app, err := enrichApp(ds, appName, processStats)
	if err != nil {
		c.PureJSON(http.StatusNotFound, &ControlPlaneError{
			ErrorCode: ErrCodeGetFailed,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusOK, &app)
}

// Install an app
//
//	@Summary		Install app
//	@Description	Install an app
//	@Tags			Apps
//	@Accept			json
//	@Produce		json
//	@Param			request	body		AppInstallRequest	true	"Install request"
//	@Success		200		{object}	AppResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/apps/ [post]
func (h *AppHandler) Install(c *gin.Context) {
	var req AppInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     err.Error(),
		})
		return
	}

	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	repoOpts := apps.RepoOpts{
		Branch: req.Branch,
		Tag:    req.Tag,
		Commit: req.Commit,
	}
	app, err := ds.GetAppManager().InstallRepo(req.RepoURL, &repoOpts, req.Force)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeInstallFailed,
			Error:     err.Error(),
		})
		return
	}

	result, err := enrichApp(ds, app.Name, false)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeInstallFailed,
			Error:     err.Error(),
		})
		return
	}
	c.PureJSON(http.StatusOK, &result)
}

// Uninstall an app
//
//	@Summary		Uninstall app
//	@Description	Uninstall an app
//	@Tags			Apps
//	@Produce		json
//	@Param			appName	path	string	true	"App name"
//	@Success		204
//	@Failure		400	{object}	ControlPlaneError
//	@Failure		401	{object}	ControlPlaneError
//	@Failure		403	{object}	ControlPlaneError
//	@Failure		409	{object}	ControlPlaneError
//	@Failure		429	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Failure		503	{object}	ControlPlaneError
//	@Router			/v1/apps/{appName} [delete]
func (h *AppHandler) Uninstall(c *gin.Context) {
	appName := c.Param("appName")

	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	if err := ds.GetAppManager().UninstallApp(appName); err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeUninstallFailed,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusNoContent, nil)
}

// TODO everything below is a temporary hack/workaround
// Ideally we would get these data directly from the app manager / app.go

func enrichApps(ds *datasite.Datasite, apps []string) []AppResponse {
	result := make([]AppResponse, len(apps))
	for i, app := range apps {
		app, err := enrichApp(ds, app, false)
		if err != nil {
			continue
		}
		result[i] = app
	}
	return result
}

func enrichApp(ds *datasite.Datasite, appName string, processStats bool) (AppResponse, error) {
	absPathFromFsRoot := filepath.Join(ds.GetAppManager().AppsDir, appName)
	relPathFromWorkspaceRoot, _ := filepath.Rel(ds.GetWorkspace().Root, absPathFromFsRoot)
	absPath := filepath.Join("/", filepath.ToSlash(relPathFromWorkspaceRoot))

	process, err := findProcess(ds, appName)
	if err != nil {
		return AppResponse{}, err
	}
	if process == nil {
		return AppResponse{
			Name:   appName,
			Path:   absPath,
			Status: AppStatusStopped,
		}, nil
	}

	app := AppResponse{
		Name:   appName,
		Path:   absPath,
		Status: AppStatusRunning,
		PID:    process.Pid,
		Ports:  getListenPorts(process),
	}

	if processStats {
		stats, err := getProcessStats(process)
		if err != nil {
			return app, nil
		}
		app.ProcessStats = &stats
	}

	return app, nil
}

func findProcess(ds *datasite.Datasite, appName string) (*process.Process, error) {
	appPath := filepath.Join(ds.GetAppManager().AppsDir, appName)
	runScriptPath, err := apps.GetRunScript(appPath)
	if err != nil {
		return nil, fmt.Errorf("app not found")
	}
	currentPid := os.Getpid()
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("failed to get processes: %w", err)
	}
	// recursively print all child processes of currentPid
	for _, p := range processes {
		ppid, err := p.Ppid()
		if err != nil {
			return nil, fmt.Errorf("failed to get ppid: %w", err)
		}
		if ppid != int32(currentPid) {
			continue
		}
		cmdline, err := p.Cmdline()
		if err != nil {
			return nil, fmt.Errorf("failed to get cmdline: %w", err)
		}
		if strings.Contains(cmdline, runScriptPath) {
			return p, nil
		}
	}
	return nil, nil
}

func getListenPorts(process *process.Process) []int64 {
	ports := make([]int64, 0)

	// Recursively travel down the process tree and return the port of all connections that is not 0
	connections, _ := process.Connections()
	for _, connection := range connections {
		if connection.Laddr.Port != 0 && connection.Status == "LISTEN" {
			port := int64(connection.Laddr.Port)
			if !slices.Contains(ports, port) {
				ports = append(ports, port)
			}
		}
	}
	children, _ := process.Children()
	for _, child := range children {
		childPorts := getListenPorts(child)
		ports = append(ports, childPorts...)
	}
	slices.Sort(ports)
	return ports
}

func getProcessStats(p *process.Process) (ProcessStats, error) {
	// Get process name
	processName, err := p.Name()
	if err != nil {
		return ProcessStats{}, fmt.Errorf("failed to get process name: %w", err)
	}

	status, err := p.Status()
	if err != nil {
		status = []string{}
	}

	// Get command line
	cmdline, err := p.CmdlineSlice()
	if err != nil {
		cmdline = []string{} // Empty slice if we can't get cmdline
	}

	// Get working directory
	cwd, err := p.Cwd()
	if err != nil {
		cwd = "" // Empty string if we can't get cwd
	}

	// Get environment variables
	environ, err := p.Environ()
	if err != nil {
		environ = []string{} // Empty slice if we can't get environ
	}

	// Get executable path
	exe, err := p.Exe()
	if err != nil {
		exe = "" // Empty string if we can't get exe
	}

	// Get group IDs
	gids, err := p.Gids()
	if err != nil {
		gids = []uint32{} // Empty slice if we can't get gids
	}

	// Get user IDs
	uids, err := p.Uids()
	if err != nil {
		uids = []uint32{} // Empty slice if we can't get uids
	}

	// Get nice value
	nice, err := p.Nice()
	if err != nil {
		nice = 0 // Default nice value if we can't get it
	}

	// Get username
	username, err := p.Username()
	if err != nil {
		username = "" // Empty string if we can't get username
	}

	// Get connections
	connections, err := p.Connections()
	if err != nil || len(connections) == 0 {
		connections = []psnet.ConnectionStat{}
	}

	cpuPercent, err := p.CPUPercent()
	if err != nil {
		cpuPercent = 0
	}

	// Get CPU times
	cpuTimes, err := p.Times()
	if err != nil {
		cpuTimes = nil // Nil if we can't get CPU times
	}

	// Get number of threads
	numThreads, err := p.NumThreads()
	if err != nil {
		numThreads = 0 // Default to 0 if we can't get num threads
	}

	memoryPercent, err := p.MemoryPercent()
	if err != nil {
		memoryPercent = 0
	}

	// Get memory info
	memoryInfo, err := p.MemoryInfo()
	if err != nil {
		memoryInfo = nil // Nil if we can't get memory info
	}

	createTime, err := p.CreateTime()
	var uptime int64
	if err != nil {
		uptime = 0
	} else {
		now := time.Now().UnixMilli()
		uptime = now - createTime
	}

	childProcesses, err := p.Children()
	if err != nil {
		childProcesses = []*process.Process{}
	}
	children := make([]ProcessStats, len(childProcesses))
	for i, child := range childProcesses {
		childStats, err := getProcessStats(child)
		if err != nil {
			continue
		}
		children[i] = childStats
	}

	return ProcessStats{
		ProcessName:   processName,
		PID:           p.Pid,
		Status:        status,
		Cmdline:       cmdline,
		CWD:           cwd,
		Environ:       environ,
		Exe:           exe,
		Gids:          gids,
		Uids:          uids,
		Nice:          nice,
		Username:      username,
		Connections:   connections,
		CPUPercent:    cpuPercent,
		CPUTimes:      cpuTimes,
		NumThreads:    numThreads,
		MemoryPercent: memoryPercent,
		MemoryInfo:    memoryInfo,
		Uptime:        uptime,
		Children:      children,
	}, nil
}
