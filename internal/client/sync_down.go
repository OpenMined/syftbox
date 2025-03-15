package client

import (
	"context"
	"log/slog"

	"github.com/yashgorana/syftbox-go/internal/message"
)

func (sm *SyncManager) handleSocketEvents(ctx context.Context) {
	sm.wsMessages = sm.api.SubscribeEvents()
	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-sm.wsMessages:
			if !ok {
				return
			}

			switch msg.Type {
			case message.MsgError:
				sm.handleError(msg)
			case message.MsgFileWrite:
				sm.handleFileWrite(msg)
			// case message.MsgFileDelete:
			// 	sm.handleFileDelete(msg)
			default:
				slog.Info("websocket message", "type", msg.Type, "data", msg.Data)
			}
		}
	}
}

func (sm *SyncManager) handleError(msg *message.Message) {
	errMsg, _ := msg.Data.(message.Error)
	slog.Info("websocket ERROR", "errCode", errMsg.Code, "path", errMsg.Path, "message", errMsg.Message)

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
	path := sm.datasite.AbsolutePath(createMsg.Path)
	slog.Info("websocket file write", "path", path, "size", createMsg.Length, "etag", createMsg.Etag)

	sm.ignorePath(path)

	etag, err := WriteFile(path, createMsg.Content)
	if err != nil {
		slog.Error("websocket file write", "error", err)
	} else if etag != createMsg.Etag {
		slog.Warn("websocket file write etag mismatch", "expected", createMsg.Etag, "actual", etag)
	}
}
