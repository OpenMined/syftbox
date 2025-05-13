package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/apps"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

const (
	ErrCodeListFailed      = "ERR_LIST_FAILED"
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

// List all installed apps
//
//	@Summary		List apps
//	@Description	List all installed apps
//	@Tags			app
//	@Produce		json
//	@Success		200	{object}	AppListResponse
//	@Failure		500	{object}	ControlPlaneError
//	@Failure		503	{object}	ControlPlaneError
//	@Router			/v1/app/list [get]
func (h *AppHandler) List(c *gin.Context) {
	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	apps, err := ds.GetAppManager().ListApps()
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeListFailed,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusOK, &AppListResponse{
		Apps: apps,
	})
}

// Install an app
//
//	@Summary		Install app
//	@Description	Install an app
//	@Tags			app
//	@Accept			json
//	@Produce		json
//	@Param			request	body		AppInstallRequest	true	"Install request"
//	@Success		200		{object}	ControlPlaneResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/app/install [post]
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
	_, err = ds.GetAppManager().InstallRepo(req.RepoURL, &repoOpts, req.Force)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeInstallFailed,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusOK, &ControlPlaneResponse{
		Code: CodeOk,
	})
}

// Uninstall an app
//
//	@Summary		Uninstall app
//	@Description	Uninstall an app
//	@Tags			app
//	@Produce		json
//	@Param			appName	query		string	true	"App name"
//	@Success		200		{object}	ControlPlaneResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/app/uninstall [post]
func (h *AppHandler) Uninstall(c *gin.Context) {
	var req AppUninstallRequest
	if err := c.ShouldBindQuery(&req); err != nil {
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

	if err := ds.GetAppManager().UninstallApp(req.AppName); err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeUnknownError,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusOK, &ControlPlaneResponse{
		Code: CodeOk,
	})
}
