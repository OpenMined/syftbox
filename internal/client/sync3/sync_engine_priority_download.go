package sync3

import (
	"log/slog"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (se *SyncEngine) handlePriorityDownload(msg *syftmsg.Message) {
	createMsg, _ := msg.Data.(syftmsg.FileWrite)
	slog.Info("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "path", createMsg.Path, "size", createMsg.Length, "etag", createMsg.ETag)

	localPath := se.workspace.DatasiteAbsPath(createMsg.Path)
	etag, err := WriteFile(localPath, createMsg.Content)
	if err != nil {
		slog.Error("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "error", err)
	} else if etag != createMsg.ETag {
		slog.Error("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "expected", createMsg.ETag, "actual", etag)
	}

	metadata := &FileMetadata{
		Path:         createMsg.Path,
		ETag:         createMsg.ETag,
		Size:         createMsg.Length,
		LastModified: time.Now(),
		Version:      "",
	}

	// update journal and lastLocalState
	se.journal.Set(metadata)
	se.lastLocalState[createMsg.Path] = metadata
}
