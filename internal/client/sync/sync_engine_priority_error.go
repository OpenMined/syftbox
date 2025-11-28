package sync

import (
	"log/slog"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (se *SyncEngine) handlePriorityError(msg *syftmsg.Message) {
	// unwrap the message
	errMsg, _ := msg.Data.(syftmsg.Error)

	// set sync status
	syncRelPath := SyncPath(errMsg.Path)
	se.syncStatus.SetSyncing(syncRelPath)
	slog.Info("sync", "type", SyncPriority, "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "message", errMsg.Message)

	// handle the error
	switch errMsg.Code {
	case 403:
		// mark the file as rejected
		localPath := se.workspace.DatasiteAbsPath(errMsg.Path)
		if markedPath, err := SetMarker(localPath, Rejected); err != nil {
			se.syncStatus.SetError(syncRelPath, err)
			slog.Warn("sync", "type", SyncPriority, "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "error", err, "DEBUG_REJECTION_REASON", "priority_error_403_SetMarker_failed", "DEBUG_ERROR_MESSAGE", errMsg.Message)
		} else {
			// Successfully marked as rejected
			slog.Warn("sync", "type", SyncPriority, "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "movedTo", markedPath, "DEBUG_REJECTION_REASON", "priority_error_403_from_server", "DEBUG_ERROR_MESSAGE", errMsg.Message)
			se.syncStatus.SetRejected(syncRelPath)
		}
	default:
		// mark as completed for unknown error codes
		se.syncStatus.SetCompleted(syncRelPath)
		slog.Debug("sync", "type", SyncPriority, "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code)
	}
}
