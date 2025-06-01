package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

const (
	ErrCodeListFailed      = "ERR_LIST_FAILED"
	ErrCodeGetFailed       = "ERR_GET_FAILED"
	ErrCodeInstallFailed   = "ERR_INSTALL_FAILED"
	ErrCodeUninstallFailed = "ERR_UNINSTALL_FAILED"
	ErrCodeStartFailed     = "ERR_START_FAILED"
	ErrCodeStopFailed      = "ERR_STOP_FAILED"
	ErrAlreadyStopped      = "ERR_ALREADY_STOPPED"
	ErrAlreadyRunning      = "ERR_ALREADY_RUNNING"
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
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, err)
		return
	}

	runningApps := ds.GetAppScheduler().ListRunningApps()

	appResponses := make([]*AppResponse, 0)
	for _, app := range runningApps {
		appResponse, err := NewAppResponse(app, false)
		if err != nil {
			// just record this error for logging
			c.Error(fmt.Errorf("failed to get app response: %s %w", app.ID, err))
			continue
		}
		appResponses = append(appResponses, appResponse)
	}

	c.PureJSON(http.StatusOK, &AppListResponse{
		Apps: appResponses,
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
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, err)
		return
	}

	app, err := ds.GetAppScheduler().GetApp(appName)
	if err != nil {
		AbortWithError(c, http.StatusNotFound, ErrCodeGetFailed, err)
		return
	}

	appResponse, err := NewAppResponse(app, processStats)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeGetFailed, err)
		return
	}
	c.PureJSON(http.StatusOK, &appResponse)
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
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, err)
		return
	}

	ds, err := h.mgr.Get()
	if err != nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, err)
		return
	}

	app, err := ds.GetAppManager().InstallApp(c.Request.Context(), apps.AppInstallOpts{
		URI:    req.RepoURL,
		Branch: req.Branch,
		Tag:    req.Tag,
		Commit: req.Commit,
		Force:  req.Force,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeInstallFailed, err)
		return
	}

	c.PureJSON(http.StatusOK, &AppResponse{
		Name:   app.ID,
		Path:   app.Path,
		Status: apps.StatusNew,
		PID:    -1,
		Ports:  []uint32{},
	})
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
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, err)
		return
	}

	if _, err := ds.GetAppManager().UninstallApp(appName); err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUninstallFailed, err)
		return
	}

	c.PureJSON(http.StatusNoContent, nil)
}

// Start an app
//
//	@Summary		Start app
//	@Description	Start an app
//	@Tags			Apps
//	@Produce		json
//	@Param			appName	path		string	true	"App name"
//	@Success		200		{object}	AppResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/apps/{appName}/start [post]
func (h *AppHandler) Start(c *gin.Context) {
	appName := c.Param("appName")

	ds, err := h.mgr.Get()
	if err != nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, err)
		return
	}

	scheduler := ds.GetAppScheduler()

	app, err := scheduler.StartApp(appName)
	if err != nil {
		var status int
		switch {
		case errors.Is(err, apps.ErrAlreadyRunning):
			status = http.StatusConflict
		case errors.Is(err, apps.ErrAppNotFound):
			status = http.StatusNotFound
		default:
			status = http.StatusInternalServerError
		}
		AbortWithError(c, status, ErrCodeStartFailed, err)
		return
	}

	// give the app a chance to start
	time.Sleep(1 * time.Second)

	// turn into app response
	appResponse, err := NewAppResponse(app, false)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, ErrCodeStartFailed, err)
		return
	}

	c.PureJSON(http.StatusCreated, &appResponse)
}

// Stop an app
//
//	@Summary		Stop app
//	@Description	Stop an app
//	@Tags			Apps
//	@Produce		json
//	@Param			appName	path		string	true	"App name"
//	@Success		200		{object}	AppResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/apps/{appName}/stop [post]
func (h *AppHandler) Stop(c *gin.Context) {
	appName := c.Param("appName")

	ds, err := h.mgr.Get()
	if err != nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, err)
		return
	}

	scheduler := ds.GetAppScheduler()

	app, err := scheduler.StopApp(appName)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, apps.ErrNotRunning) {
			status = http.StatusConflict
		}
		AbortWithError(c, status, ErrCodeStopFailed, err)
		return
	}

	info := app.Info()

	c.PureJSON(http.StatusOK, &AppResponse{
		ID:     info.ID,
		Name:   info.Name,
		Path:   info.Path,
		Info:   info,
		Status: app.GetStatus(),
	})
}
