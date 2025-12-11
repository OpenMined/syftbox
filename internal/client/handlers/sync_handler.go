package handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/client/sync"
)

type SyncHandler struct {
	datasiteMgr *datasitemgr.DatasiteManager
}

func NewSyncHandler(datasiteMgr *datasitemgr.DatasiteManager) *SyncHandler {
	return &SyncHandler{datasiteMgr: datasiteMgr}
}

// Status godoc
//
//	@Summary		Get sync status
//	@Description	Returns the current sync status for all files
//	@Tags			sync
//	@Produce		json
//	@Success		200	{object}	SyncStatusResponse
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/sync/status [get]
//	@Security		APIToken
func (h *SyncHandler) Status(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	syncStatus := ds.GetSyncManager().GetSyncStatus()
	if syncStatus == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("sync not available"))
		return
	}

	allStatus := syncStatus.GetAllStatus()

	files := make([]SyncFileStatus, 0, len(allStatus))
	var pendingCount, syncingCount, completedCount, errorCount int

	for path, status := range allStatus {
		var errMsg string
		if status.Error != nil {
			errMsg = status.Error.Error()
		}

		files = append(files, SyncFileStatus{
			Path:          path.String(),
			State:         string(status.SyncState),
			ConflictState: string(status.ConflictState),
			Progress:      status.Progress * 100.0,
			Error:         errMsg,
			ErrorCount:    status.ErrorCount,
			UpdatedAt:     status.LastUpdated,
		})

		switch status.SyncState {
		case sync.SyncStatePending:
			pendingCount++
		case sync.SyncStateSyncing:
			syncingCount++
		case sync.SyncStateCompleted:
			completedCount++
		case sync.SyncStateError:
			errorCount++
		}
	}

	c.JSON(http.StatusOK, SyncStatusResponse{
		Files: files,
		Summary: SyncSummary{
			Pending:   pendingCount,
			Syncing:   syncingCount,
			Completed: completedCount,
			Error:     errorCount,
		},
	})
}

// StatusByPath godoc
//
//	@Summary		Get sync status for a specific path
//	@Description	Returns the sync status for a specific file path
//	@Tags			sync
//	@Produce		json
//	@Param			path	query		string	true	"File path"
//	@Success		200		{object}	SyncFileStatus
//	@Failure		404		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Router			/v1/sync/status/file [get]
//	@Security		APIToken
func (h *SyncHandler) StatusByPath(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		AbortWithError(c, http.StatusBadRequest, ErrCodeBadRequest, errors.New("path is required"))
		return
	}

	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	syncStatus := ds.GetSyncManager().GetSyncStatus()
	if syncStatus == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("sync not available"))
		return
	}

	status, exists := syncStatus.GetStatus(sync.SyncPath(path))
	if !exists {
		AbortWithError(c, http.StatusNotFound, ErrCodeBadRequest, errors.New("path not found in sync status"))
		return
	}

	var errMsg string
	if status.Error != nil {
		errMsg = status.Error.Error()
	}

	c.JSON(http.StatusOK, SyncFileStatus{
		Path:          path,
		State:         string(status.SyncState),
		ConflictState: string(status.ConflictState),
		Progress:      status.Progress * 100.0,
		Error:         errMsg,
		ErrorCount:    status.ErrorCount,
		UpdatedAt:     status.LastUpdated,
	})
}

// Events godoc
//
//	@Summary		Stream sync events
//	@Description	Server-sent events stream for real-time sync updates
//	@Tags			sync
//	@Produce		text/event-stream
//	@Success		200	{string}	string	"SSE stream"
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/sync/events [get]
//	@Security		APIToken
func (h *SyncHandler) Events(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	syncStatus := ds.GetSyncManager().GetSyncStatus()
	if syncStatus == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("sync not available"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	eventCh := syncStatus.Subscribe()
	defer syncStatus.Unsubscribe(eventCh)

	ctx := c.Request.Context()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case event, ok := <-eventCh:
			if !ok {
				return false
			}

			var errMsg string
			if event.Status.Error != nil {
				errMsg = event.Status.Error.Error()
			}

			data := SyncFileStatus{
				Path:          event.Path.String(),
				State:         string(event.Status.SyncState),
				ConflictState: string(event.Status.ConflictState),
				Progress:      event.Status.Progress * 100.0,
				Error:         errMsg,
				ErrorCount:    event.Status.ErrorCount,
				UpdatedAt:     event.Status.LastUpdated,
			}

			c.SSEvent("sync", data)
			return true
		}
	})
}

// TriggerSync godoc
//
//	@Summary		Trigger immediate sync
//	@Description	Triggers an immediate sync cycle
//	@Tags			sync
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Failure		500	{object}	ControlPlaneError
//	@Router			/v1/sync/now [post]
//	@Security		APIToken
func (h *SyncHandler) TriggerSync(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	ds.GetSyncManager().TriggerSync()

	c.JSON(http.StatusOK, gin.H{"status": "sync triggered"})
}
