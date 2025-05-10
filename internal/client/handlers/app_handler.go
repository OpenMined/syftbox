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
	"github.com/shirou/gopsutil/v4/process"
)

const (
	ErrCodeListFailed      = "ERR_LIST_FAILED"
	ErrCodeGetFailed       = "ERR_GET_FAILED"
	ErrCodeInstallFailed   = "ERR_INSTALL_FAILED"
	ErrCodeUninstallFailed = "ERR_UNINSTALL_FAILED"
)

type AppHandler struct {
	mgr *datasitemgr.DatasiteManger
}

func NewAppHandler(mgr *datasitemgr.DatasiteManger) *AppHandler {
	return &AppHandler{
		mgr: mgr,
	}
}

// @Summary		List apps
// @Description	List all installed apps
// @Tags			Apps
// @Produce		json
// @Success		200	{object}	AppListResponse
// @Failure		400	{object}	ControlPlaneError
// @Failure		401	{object}	ControlPlaneError
// @Failure		403	{object}	ControlPlaneError
// @Failure		409	{object}	ControlPlaneError
// @Failure		429	{object}	ControlPlaneError
// @Failure		500	{object}	ControlPlaneError
// @Failure		503	{object}	ControlPlaneError
// @Router			/v1/apps/ [get]
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

// @Summary		Get app
// @Description	Get an app
// @Tags			Apps
// @Produce		json
// @Param			appName	path		string	true	"App name"
// @Success		200		{object}	AppResponse
// @Failure		400		{object}	ControlPlaneError
// @Failure		401		{object}	ControlPlaneError
// @Failure		403		{object}	ControlPlaneError
// @Failure		409		{object}	ControlPlaneError
// @Failure		429		{object}	ControlPlaneError
// @Failure		500		{object}	ControlPlaneError
// @Failure		503		{object}	ControlPlaneError
// @Router			/v1/apps/{appName} [get]
func (h *AppHandler) Get(c *gin.Context) {
	appName := c.Param("appName")

	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	app, err := enrichApp(ds, appName)
	if err != nil {
		c.PureJSON(http.StatusNotFound, &ControlPlaneError{
			ErrorCode: ErrCodeGetFailed,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusOK, &app)
}

// @Summary		Install app
// @Description	Install an app
// @Tags			Apps
// @Accept			json
// @Produce		json
// @Param			request	body		AppInstallRequest	true	"Install request"
// @Success		200		{object}	AppResponse
// @Failure		400		{object}	ControlPlaneError
// @Failure		401		{object}	ControlPlaneError
// @Failure		403		{object}	ControlPlaneError
// @Failure		409		{object}	ControlPlaneError
// @Failure		429		{object}	ControlPlaneError
// @Failure		500		{object}	ControlPlaneError
// @Failure		503		{object}	ControlPlaneError
// @Router			/v1/apps/ [post]
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

	result, err := enrichApp(ds, app.Name)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeInstallFailed,
			Error:     err.Error(),
		})
		return
	}
	c.PureJSON(http.StatusOK, &result)
}

// @Summary		Uninstall app
// @Description	Uninstall an app
// @Tags			Apps
// @Produce		json
// @Param			appName	path	string	true	"App name"
// @Success		204
// @Failure		400	{object}	ControlPlaneError
// @Failure		401	{object}	ControlPlaneError
// @Failure		403	{object}	ControlPlaneError
// @Failure		409	{object}	ControlPlaneError
// @Failure		429	{object}	ControlPlaneError
// @Failure		500	{object}	ControlPlaneError
// @Failure		503	{object}	ControlPlaneError
// @Router			/v1/apps/{appName} [delete]
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
		app, err := enrichApp(ds, app)
		if err != nil {
			continue
		}
		result[i] = app
	}
	return result
}

func enrichApp(ds *datasite.Datasite, appName string) (AppResponse, error) {
	appPath := filepath.Join(ds.GetAppManager().AppsDir, appName)

	if !apps.IsValidApp(appPath) {
		return AppResponse{}, fmt.Errorf("app not found")
	}

	relPath, _ := filepath.Rel(ds.GetWorkspace().Root, appPath)
	relPath = filepath.Join("/", filepath.ToSlash(relPath))

	runScriptPath, _ := apps.GetRunScript(appPath)
	process, err := findProcess(runScriptPath)
	if err != nil {
		return AppResponse{}, err
	}
	if process == nil {
		return AppResponse{
			Name:   appName,
			Path:   relPath,
			Status: AppStatusStopped,
		}, nil
	}

	ports, err := getPort(process)
	if err != nil {
		return AppResponse{}, err
	}

	cpu, err := process.CPUPercent()
	if err != nil {
		return AppResponse{}, err
	}

	memory, err := process.MemoryPercent()
	if err != nil {
		return AppResponse{}, err
	}

	createTime, err := process.CreateTime()
	if err != nil {
		return AppResponse{}, err
	}
	now := time.Now().UnixMilli()
	uptime := now - createTime

	return AppResponse{
		Name:   appName,
		Path:   relPath,
		Status: AppStatusRunning,
		PID:    process.Pid,
		Ports:  ports,
		CPU:    cpu,
		Memory: memory,
		Uptime: uptime,
	}, nil
}

func findProcess(runScriptPath string) (*process.Process, error) {
	currentPid := os.Getpid()
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("failed to get processes: %w", err)
	}
	// recursively print all child processes of currentPid
	for _, process := range processes {
		ppid, err := process.Ppid()
		if err != nil {
			return nil, fmt.Errorf("failed to get ppid: %w", err)
		}
		if ppid != int32(currentPid) {
			continue
		}
		cmdline, err := process.Cmdline()
		if err != nil {
			return nil, fmt.Errorf("failed to get cmdline: %w", err)
		}
		if strings.Contains(cmdline, runScriptPath) {
			return process, nil
		}
	}
	return nil, nil
}

func getPort(process *process.Process) ([]int64, error) {
	// Recursively travel down the process tree and return the port of all connections that is not 0
	connections, err := process.Connections()
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %w", err)
	}
	ports := make([]int64, 0)
	for _, connection := range connections {
		if connection.Laddr.Port != 0 {
			port := int64(connection.Laddr.Port)
			if !slices.Contains(ports, port) {
				ports = append(ports, port)
			}
		}
	}
	children, err := process.Children()
	if err != nil {
		return ports, fmt.Errorf("failed to get children: %w", err)
	}
	for _, child := range children {
		port, err := getPort(child)
		if err != nil {
			return ports, fmt.Errorf("failed to get port: %w", err)
		}
		ports = append(ports, port...)
	}
	return ports, nil
}
