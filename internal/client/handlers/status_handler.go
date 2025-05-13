package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/version"
)

// StatusHandler handles status-related endpoints
type StatusHandler struct {
	mgr *datasitemgr.DatasiteManager
}

// NewStatusHandler creates a new status handler
func NewStatusHandler(mgr *datasitemgr.DatasiteManager) *StatusHandler {
	return &StatusHandler{
		mgr: mgr,
	}
}

// GetStatus returns the status of the service
//
//	@Summary		Get status
//	@Description	Returns the status of the service
//	@Tags			status
//	@Produce		json
//	@Success		200	{object}	StatusResponse
//	@Router			/v1/status [get]
func (h *StatusHandler) Status(ctx *gin.Context) {
	// this is unlikely to happen, but just in case
	if h.mgr == nil {
		ctx.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeUnknownError,
			Error:     "datasite manager not initialized",
		})
		return
	}

	dsInfo := h.mgr.Status()
	hasConfig := dsInfo.Status == datasitemgr.DatasiteStatusProvisioned

	ctx.PureJSON(http.StatusOK, &StatusResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   version.Version,
		Revision:  version.Revision,
		BuildDate: version.BuildDate,
		HasConfig: hasConfig,
		Datasite: &DatasiteInfo{
			Status: string(dsInfo.Status),
			Error:  dsInfo.Error,
		},
	})
}
