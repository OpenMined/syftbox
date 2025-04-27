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
	mgr *datasitemgr.DatasiteManger
}

// NewStatusHandler creates a new status handler
func NewStatusHandler(mgr *datasitemgr.DatasiteManger) *StatusHandler {
	return &StatusHandler{
		mgr: mgr,
	}
}

// @Summary		Get status
// @Description	Returns the status of the service
// @Tags			status
// @Produce		json
// @Success		200	{object}	StatusResponse
// @Router			/status [get]
func (h *StatusHandler) Status(ctx *gin.Context) {
	var hasConfig bool = false

	// Get the datasite configuration if available
	if h.mgr != nil {
		ds, err := h.mgr.Get()
		if err == nil && ds != nil {
			hasConfig = true
		}
	}

	ctx.PureJSON(http.StatusOK, &StatusResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   version.Version,
		Revision:  version.Revision,
		BuildDate: version.BuildDate,
		HasConfig: hasConfig,
	})
}
