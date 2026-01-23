package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

// LatencyHandler handles latency-related endpoints
type LatencyHandler struct {
	mgr *datasitemgr.DatasiteManager
}

// NewLatencyHandler creates a new latency handler
func NewLatencyHandler(mgr *datasitemgr.DatasiteManager) *LatencyHandler {
	return &LatencyHandler{mgr: mgr}
}

// GetLatency returns server latency statistics
//
//	@Summary		Get server latency stats
//	@Description	Returns server round-trip time statistics
//	@Tags			Stats
//	@Produce		json
//	@Success		200	{object}	datasitemgr.LatencySnapshot
//	@Router			/v1/stats/latency [get]
func (h *LatencyHandler) GetLatency(c *gin.Context) {
	stats := h.mgr.GetLatencyStats()
	if stats == nil {
		c.PureJSON(http.StatusOK, datasitemgr.LatencySnapshot{
			Samples: []uint64{},
		})
		return
	}
	c.PureJSON(http.StatusOK, stats.Snapshot())
}
