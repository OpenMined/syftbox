package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	clientsync "github.com/openmined/syftbox/internal/client/sync"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/version"
)

var processStartedAt = time.Now().UTC()

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
//	@Tags			Status
//	@Produce		json
//	@Success		200	{object}	StatusResponse
//	@Failure		503	{object}	ControlPlaneError
//	@Router			/v1/status [get]
func (h *StatusHandler) Status(c *gin.Context) {
	// this is unlikely to happen, but just in case
	if h.mgr == nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeUnknownError,
			Error:     "datasite manager not initialized",
		})
		return
	}

	var dsConfig *DatasiteConfig
	var errorMessage string

	status := h.mgr.Status()
	if status.Status == datasitemgr.DatasiteStatusProvisioning || status.Status == datasitemgr.DatasiteStatusProvisioned {
		cfg := status.Datasite.GetConfig()
		// share a copy of the config. DO NOT INCLUDE REFRESH TOKEN!
		dsConfig = &DatasiteConfig{
			DataDir:   cfg.DataDir,
			Email:     cfg.Email,
			ServerURL: cfg.ServerURL,
		}
	} else if status.DatasiteError != nil {
		errorMessage = status.DatasiteError.Error()
	}

	c.PureJSON(http.StatusOK, &StatusResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   version.Version,
		Revision:  version.Revision,
		BuildDate: version.BuildDate,
		Datasite: &DatasiteInfo{
			Status: string(status.Status),
			Error:  errorMessage,
			Config: dsConfig,
		},
		Runtime: h.buildRuntime(status),
	})
}

func (h *StatusHandler) buildRuntime(status *datasitemgr.DatasiteManagerStatus) *RuntimeInfo {
	if status == nil {
		return nil
	}
	ds := status.Datasite

	var serverURL, clientURL string
	var clientTokenConfigured bool
	if ds != nil {
		cfg := ds.GetConfig()
		if cfg != nil {
			serverURL = cfg.ServerURL
			clientURL = cfg.ClientURL
			clientTokenConfigured = cfg.ClientToken != ""
		}
	}

	clientInfo := &ClientInfo{
		Version:     version.Version,
		Revision:    version.Revision,
		BuildDate:   version.BuildDate,
		StartedAt:   processStartedAt.Format(time.RFC3339Nano),
		UptimeSec:   int64(time.Since(processStartedAt).Seconds()),
		ServerURL:   serverURL,
		ClientURL:   clientURL,
		ClientToken: clientTokenConfigured,
	}

	// If no datasite yet, still return client info.
	if ds == nil {
		return &RuntimeInfo{Client: clientInfo}
	}

	// Websocket stats
	sdk := ds.GetSDK()
	wsSnap := sdk.Events.Stats()
	wsInfo := &WebsocketInfo{
		Connected:        wsSnap.Connected,
		Encoding:         wsSnap.Encoding,
		ReconnectAttempt: wsSnap.ReconnectAttempt,
		Reconnects:       wsSnap.Reconnects,
		TxQueueLen:       wsSnap.TxQueueLen,
		RxQueueLen:       wsSnap.RxQueueLen,
		OverflowQueueLen: wsSnap.OverflowQueueLen,
		BytesSentTotal:   wsSnap.BytesSentTotal,
		BytesRecvTotal:   wsSnap.BytesRecvTotal,
		LastError:        wsSnap.LastError,
	}
	// Convert ns timestamps to RFC3339
	setIfNonZero := func(ns int64) string {
		if ns == 0 {
			return ""
		}
		return time.Unix(0, ns).UTC().Format(time.RFC3339Nano)
	}
	wsInfo.ConnectedAt = setIfNonZero(wsSnap.ConnectedAtNs)
	wsInfo.DisconnectedAt = setIfNonZero(wsSnap.DisconnectedAtNs)
	wsInfo.LastSentAt = setIfNonZero(wsSnap.LastSentAtNs)
	wsInfo.LastRecvAt = setIfNonZero(wsSnap.LastRecvAtNs)
	wsInfo.LastPingAt = setIfNonZero(wsSnap.LastPingAtNs)

	// HTTP stats (sync/uploads/downloads)
	httpSnap := sdk.HTTPStats()
	httpInfo := &HTTPInfo{
		BytesSentTotal: httpSnap.BytesSentTotal,
		BytesRecvTotal: httpSnap.BytesRecvTotal,
		LastError:      httpSnap.LastError,
	}
	httpInfo.LastSentAt = setIfNonZero(httpSnap.LastSentAtNs)
	httpInfo.LastRecvAt = setIfNonZero(httpSnap.LastRecvAtNs)

	// Sync stats
	sm := ds.GetSyncManager()
	ss := sm.GetSyncStatus()
	tracked := len(ss.GetAllStatus())
	syncInfo := &SyncInfo{
		LastFullSyncAt:  sm.LastSyncTime().Format(time.RFC3339Nano),
		TrackedFiles:    tracked,
		SyncingFiles:    ss.GetSyncingFileCount(),
		ConflictedFiles: ss.GetConflictedFileCount(),
		RejectedFiles:   ss.GetRejectedFileCount(),
	}
	if sm.LastSyncTime().IsZero() {
		syncInfo.LastFullSyncAt = ""
	}

	// Upload queue stats
	ur := sm.GetUploadRegistry()
	uploads := ur.List()
	uInfo := &UploadsInfo{Total: len(uploads)}
	for _, u := range uploads {
		switch u.State {
		case clientsync.UploadStateUploading:
			uInfo.Uploading++
		case clientsync.UploadStatePending:
			uInfo.Pending++
		case clientsync.UploadStatePaused:
			uInfo.Paused++
		case clientsync.UploadStateError:
			uInfo.Error++
		}
	}

	return &RuntimeInfo{
		Client:    clientInfo,
		Websocket: wsInfo,
		HTTP:      httpInfo,
		Sync:      syncInfo,
		Uploads:   uInfo,
	}
}
