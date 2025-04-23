package sync3

import (
	"log/slog"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (se *SyncEngine) handlePriorityError(msg *syftmsg.Message) {
	errMsg, _ := msg.Data.(syftmsg.Error)
	slog.Info("sync priority", "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "message", errMsg.Message)

	switch errMsg.Code {
	case 403:
		localPath := se.workspace.DatasiteAbsPath(errMsg.Path)
		if err := MarkRejected(localPath); err != nil {
			slog.Warn("sync priority", "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "error", err)
		}
	default:
		slog.Debug("sync priority", "op", OpError, "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code)
	}
}
