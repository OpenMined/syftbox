package sync

import (
	"log/slog"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (se *SyncEngine) handlePriorityDownload(msg *syftmsg.Message) {
	createMsg, _ := msg.Data.(syftmsg.FileWrite)
	slog.Info("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "path", createMsg.Path, "size", createMsg.Length, "etag", createMsg.ETag)

	se.syncStatus.SetSyncing(createMsg.Path)
	defer se.syncStatus.UnsetSyncing(createMsg.Path)

	localAbsPath := se.workspace.DatasiteAbsPath(createMsg.Path)
	etag, err := WriteFile(localAbsPath, createMsg.Content)
	if err != nil {
		slog.Error("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "error", err)
		return
	} else if etag != createMsg.ETag {
		slog.Error("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "expected", createMsg.ETag, "actual", etag)
		return
	}

	// update journal and lastLocalState
	metadata := &FileMetadata{
		Path:         createMsg.Path,
		ETag:         createMsg.ETag,
		Size:         createMsg.Length,
		LastModified: time.Now(),
		Version:      "",
	}
	se.journal.Set(metadata)
	se.lastLocalState[createMsg.Path] = metadata
}
