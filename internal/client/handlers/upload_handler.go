package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/client/sync"
)

const (
	ErrCodeUploadNotFound = "ERR_UPLOAD_NOT_FOUND"
	ErrCodeUploadFailed   = "ERR_UPLOAD_FAILED"
)

type UploadHandler struct {
	datasiteMgr *datasitemgr.DatasiteManager
}

func NewUploadHandler(datasiteMgr *datasitemgr.DatasiteManager) *UploadHandler {
	return &UploadHandler{datasiteMgr: datasiteMgr}
}

// List godoc
//
//	@Summary		List active uploads
//	@Description	Returns all active and paused uploads
//	@Tags			uploads
//	@Produce		json
//	@Success		200	{object}	UploadListResponse
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/uploads [get]
//	@Security		APIToken
func (h *UploadHandler) List(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	registry := ds.GetSyncManager().GetUploadRegistry()
	if registry == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("upload registry not available"))
		return
	}

	uploads := registry.List()
	response := make([]UploadInfoResponse, 0, len(uploads))
	for _, u := range uploads {
		response = append(response, toUploadInfoResponse(u))
	}

	c.JSON(http.StatusOK, UploadListResponse{Uploads: response})
}

// Get godoc
//
//	@Summary		Get upload details
//	@Description	Returns details for a specific upload
//	@Tags			uploads
//	@Produce		json
//	@Param			id	path		string	true	"Upload ID"
//	@Success		200	{object}	UploadInfoResponse
//	@Failure		404	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/uploads/{id} [get]
//	@Security		APIToken
func (h *UploadHandler) Get(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, errors.New("upload id is required"))
		return
	}

	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	registry := ds.GetSyncManager().GetUploadRegistry()
	if registry == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("upload registry not available"))
		return
	}

	info, err := registry.Get(id)
	if err != nil {
		if err == sync.ErrUploadNotFound {
			AbortWithError(c, http.StatusNotFound, ErrCodeUploadNotFound, errors.New("upload not found"))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUploadFailed, err)
		return
	}

	c.JSON(http.StatusOK, toUploadInfoResponse(info))
}

// Pause godoc
//
//	@Summary		Pause an upload
//	@Description	Pauses an in-progress upload
//	@Tags			uploads
//	@Produce		json
//	@Param			id	path		string	true	"Upload ID"
//	@Success		200	{object}	map[string]string
//	@Failure		404	{object}	ControlPlaneError
//	@Failure		400	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/uploads/{id}/pause [post]
//	@Security		APIToken
func (h *UploadHandler) Pause(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, errors.New("upload id is required"))
		return
	}

	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	registry := ds.GetSyncManager().GetUploadRegistry()
	if registry == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("upload registry not available"))
		return
	}

	if err := registry.Pause(id); err != nil {
		if err == sync.ErrUploadNotFound {
			AbortWithError(c, http.StatusNotFound, ErrCodeUploadNotFound, errors.New("upload not found"))
			return
		}
		AbortWithError(c, http.StatusBadRequest, ErrCodeUploadFailed, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "paused"})
}

// Resume godoc
//
//	@Summary		Resume a paused upload
//	@Description	Resumes a paused upload
//	@Tags			uploads
//	@Produce		json
//	@Param			id	path		string	true	"Upload ID"
//	@Success		200	{object}	map[string]string
//	@Failure		404	{object}	ControlPlaneError
//	@Failure		400	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/uploads/{id}/resume [post]
//	@Security		APIToken
func (h *UploadHandler) Resume(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, errors.New("upload id is required"))
		return
	}

	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	registry := ds.GetSyncManager().GetUploadRegistry()
	if registry == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("upload registry not available"))
		return
	}

	if err := registry.Resume(id); err != nil {
		if err == sync.ErrUploadNotFound {
			AbortWithError(c, http.StatusNotFound, ErrCodeUploadNotFound, errors.New("upload not found"))
			return
		}
		AbortWithError(c, http.StatusBadRequest, ErrCodeUploadFailed, err)
		return
	}

	ds.GetSyncManager().TriggerSync()

	c.JSON(http.StatusOK, gin.H{"status": "resumed"})
}

// Restart godoc
//
//	@Summary		Restart an upload
//	@Description	Restarts an upload from the beginning, clearing progress
//	@Tags			uploads
//	@Produce		json
//	@Param			id	path		string	true	"Upload ID"
//	@Success		200	{object}	map[string]string
//	@Failure		404	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/uploads/{id}/restart [post]
//	@Security		APIToken
func (h *UploadHandler) Restart(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, errors.New("upload id is required"))
		return
	}

	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	registry := ds.GetSyncManager().GetUploadRegistry()
	if registry == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("upload registry not available"))
		return
	}

	if err := registry.Restart(id); err != nil {
		if err == sync.ErrUploadNotFound {
			AbortWithError(c, http.StatusNotFound, ErrCodeUploadNotFound, errors.New("upload not found"))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUploadFailed, err)
		return
	}

	ds.GetSyncManager().TriggerSync()

	c.JSON(http.StatusOK, gin.H{"status": "restarted"})
}

// Cancel godoc
//
//	@Summary		Cancel an upload
//	@Description	Cancels and removes an upload
//	@Tags			uploads
//	@Produce		json
//	@Param			id	path		string	true	"Upload ID"
//	@Success		200	{object}	map[string]string
//	@Failure		404	{object}	ControlPlaneError
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/uploads/{id} [delete]
//	@Security		APIToken
func (h *UploadHandler) Cancel(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, errors.New("upload id is required"))
		return
	}

	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	registry := ds.GetSyncManager().GetUploadRegistry()
	if registry == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("upload registry not available"))
		return
	}

	if err := registry.Cancel(id); err != nil {
		if err == sync.ErrUploadNotFound {
			AbortWithError(c, http.StatusNotFound, ErrCodeUploadNotFound, errors.New("upload not found"))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, ErrCodeUploadFailed, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

func toUploadInfoResponse(info *sync.UploadInfo) UploadInfoResponse {
	return UploadInfoResponse{
		ID:             info.ID,
		Key:            info.Key,
		LocalPath:      info.LocalPath,
		State:          string(info.State),
		Size:           info.Size,
		UploadedBytes:  info.UploadedBytes,
		PartSize:       info.PartSize,
		PartCount:      info.PartCount,
		CompletedParts: info.CompletedParts,
		Progress:       info.Progress,
		Error:          info.Error,
		StartedAt:      info.StartedAt,
		UpdatedAt:      info.UpdatedAt,
	}
}
