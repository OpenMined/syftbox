package sync

import (
	"log/slog"
	"os"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

// handlePriorityDownload processes a file write message received with high priority,
func (se *SyncEngine) handlePriorityDownload(msg *syftmsg.Message) {
	// unwrap the message
	createMsg, _ := msg.Data.(syftmsg.FileWrite)

	// set sync status
	se.syncStatus.SetSyncing(createMsg.Path, "priority download")
	defer se.syncStatus.SetCompleted(createMsg.Path, "priority download")
	slog.Info("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "path", createMsg.Path, "size", createMsg.Length, "etag", createMsg.ETag)

	// write the file to the local datasite
	localAbsPath := se.workspace.DatasiteAbsPath(createMsg.Path)
	etag, err := writeFile(localAbsPath, createMsg.Content)
	if err != nil {
		slog.Error("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "error", err)
		return
	} else if etag != createMsg.ETag {
		// Verify content integrity using ETag.
		slog.Error("sync priority", "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "expected", createMsg.ETag, "actual", etag)
		os.Remove(localAbsPath)
		return
	}

	// Update the sync journal
	se.journal.Set(&FileMetadata{
		Path:         createMsg.Path,
		ETag:         createMsg.ETag,
		Size:         createMsg.Length,
		LastModified: time.Now(),
		Version:      "",
	})
}
