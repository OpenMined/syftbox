package sync

import (
	"log/slog"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (se *SyncEngine) handlePriorityError(msg *syftmsg.Message) {
	// unwrap the message
	errMsg, _ := msg.Data.(syftmsg.Error)

	// set sync status
	se.syncStatus.SetSyncing(errMsg.Path, "priority error")
	defer se.syncStatus.SetCompleted(errMsg.Path, "priority error")
	slog.Info("sync priority", "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "message", errMsg.Message)

	// handle the error
	switch errMsg.Code {
	case 403:
		// mark the file as rejected
		localPath := se.workspace.DatasiteAbsPath(errMsg.Path)
		if err := markRejected(localPath); err != nil {
			slog.Warn("sync priority", "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "error", err)
		}
	default:
		slog.Debug("sync priority", "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code)
	}
}
