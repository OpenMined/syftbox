package sync

import (
	"context"
	"log/slog"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (sm *SyncManager) handleSocketEvents(ctx context.Context) {
	socketEvents := sm.sdk.Events.Get()
	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-socketEvents:
			if !ok {
				slog.Debug("handleSocketEvents channel closed")
				return
			}

			switch msg.Type {
			case syftmsg.MsgSystem:
				sm.handleSystem(msg)
			case syftmsg.MsgError:
				sm.handleError(msg)
			case syftmsg.MsgFileWrite:
				sm.handleFileWrite(msg)
			case syftmsg.MsgFileDelete:
				sm.handleFileDelete(msg)
			default:
				slog.Debug("websocket unhandled type", "type", msg.Type)
			}
		}
	}
}

func (sm *SyncManager) handleSystem(msg *syftmsg.Message) {
	systemMsg := msg.Data.(syftmsg.System)
	slog.Info("handle", "msgType", msg.Type, "msgId", msg.Id, "serverVersion", systemMsg.SystemVersion)
}

func (sm *SyncManager) handleError(msg *syftmsg.Message) {
	errMsg, _ := msg.Data.(syftmsg.Error)
	slog.Info("handle", "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "message", errMsg.Message)

	switch errMsg.Code {
	case 403:
		if err := RejectFile(sm.datasite.DatasiteAbsPath(errMsg.Path)); err != nil {
			slog.Warn("reject file error", "error", err)
		}
	default:
		slog.Debug("unhandled", "code", errMsg.Code)
	}
}

func (sm *SyncManager) handleFileWrite(msg *syftmsg.Message) {
	createMsg, _ := msg.Data.(syftmsg.FileWrite)
	slog.Info("handle", "msgType", msg.Type, "msgId", msg.Id, "path", createMsg.Path, "size", createMsg.Length, "etag", createMsg.ETag)

	fullPath := sm.datasite.DatasiteAbsPath(createMsg.Path)
	sm.ignorePath(fullPath)
	etag, err := WriteFile(fullPath, createMsg.Content)
	if err != nil {
		slog.Error("handle", "msgType", msg.Type, "msgId", msg.Id, "error", err)
	} else if etag != createMsg.ETag {
		slog.Warn("handle etag mismatch", "msgType", msg.Type, "msgId", msg.Id, "expected", createMsg.ETag, "actual", etag)
	}
}

func (sm *SyncManager) handleFileDelete(msg *syftmsg.Message) {
	deleteMsg, _ := msg.Data.(syftmsg.FileDelete)
	slog.Warn("unhandled", "msgType", msg.Type, "msgId", msg.Id, "path", deleteMsg.Path)
}
