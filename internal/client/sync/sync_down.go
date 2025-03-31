package sync

import (
	"context"
	"log/slog"

	"github.com/yashgorana/syftbox-go/internal/message"
)

func (sm *SyncManager) handleSocketEvents(ctx context.Context) {
	socketEvents := sm.api.Messages()
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
			case message.MsgSystem:
				sm.handleSystem(msg)
			case message.MsgError:
				sm.handleError(msg)
			case message.MsgFileWrite:
				sm.handleFileWrite(msg)
			case message.MsgFileDelete:
				sm.handleFileDelete(msg)
			default:
				slog.Debug("websocket unhandled type", "type", msg.Type)
			}
		}
	}
}

func (sm *SyncManager) handleSystem(msg *message.Message) {
	systemMsg := msg.Data.(message.System)
	slog.Info("handle", "msgType", msg.Type, "msgId", msg.Id, "serverVersion", systemMsg.SystemVersion)
}

func (sm *SyncManager) handleError(msg *message.Message) {
	errMsg, _ := msg.Data.(message.Error)
	slog.Info("handle", "msgType", msg.Type, "msgId", msg.Id, "code", errMsg.Code, "path", errMsg.Path, "message", errMsg.Message)

	switch errMsg.Code {
	case 403:
		if err := RejectFile(sm.datasite.AbsolutePath(errMsg.Path)); err != nil {
			slog.Warn("reject file error", "error", err)
		}
	default:
		slog.Debug("unhandled", "code", errMsg.Code)
	}
}

func (sm *SyncManager) handleFileWrite(msg *message.Message) {
	createMsg, _ := msg.Data.(message.FileWrite)
	slog.Info("handle", "msgType", msg.Type, "msgId", msg.Id, "path", createMsg.Path, "size", createMsg.Length, "etag", createMsg.Etag)

	fullPath := sm.datasite.AbsolutePath(createMsg.Path)
	sm.ignorePath(fullPath)
	etag, err := WriteFile(fullPath, createMsg.Content)
	if err != nil {
		slog.Error("handle", "msgType", msg.Type, "msgId", msg.Id, "error", err)
	} else if etag != createMsg.Etag {
		slog.Warn("handle etag mismatch", "msgType", msg.Type, "msgId", msg.Id, "expected", createMsg.Etag, "actual", etag)
	}
}

func (sm *SyncManager) handleFileDelete(msg *message.Message) {
	deleteMsg, _ := msg.Data.(message.FileDelete)
	slog.Warn("unhandled", "msgType", msg.Type, "msgId", msg.Id, "path", deleteMsg.Path)
}
