package sync

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

// handlePriorityDownload processes a file write message received with high priority,
func (se *SyncEngine) handlePriorityDownload(msg *syftmsg.Message) {
	// unwrap the message
	createMsg, ok := msg.Data.(syftmsg.FileWrite)
	if !ok {
		slog.Error("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "error", "invalid message data", "data", msg.Data)
		return
	}

	syncRelPath := SyncPath(createMsg.Path)

	// If content is empty, this is a push notification (not embedded content)
	// Trigger an immediate sync to download the file
	if len(createMsg.Content) == 0 {
		slog.Info("push notification received, triggering immediate sync", "path", createMsg.Path, "etag", createMsg.ETag)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := se.runFullSync(ctx); err != nil {
				slog.Error("sync after push notification failed", "path", createMsg.Path, "error", err)
			}
		}()
		return
	}

	// set sync status
	se.syncStatus.SetSyncing(syncRelPath)
	slog.Info("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "path", createMsg.Path, "size", createMsg.Length, "etag", createMsg.ETag)

	// prep local path
	localAbsPath := se.workspace.DatasiteAbsPath(createMsg.Path)

	// a priority file was just downloaded, we don't wanna fire an event for THIS write
	se.watcher.IgnoreOnce(localAbsPath)

	// temporary directory for the file
	tmpDir := filepath.Join(se.workspace.Root, ".syft-tmp")

	// write the file to the temporary directory and
	// then move it to the local path
	err := writeFileWithIntegrityCheck(tmpDir, localAbsPath, createMsg.Content, createMsg.ETag)
	if err != nil {
		se.syncStatus.SetError(syncRelPath, err)
		slog.Error("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "error", err)
		return
	}

	// Update the sync journal
	localETag := ""
	if et, err := calculateETag(localAbsPath); err == nil {
		localETag = et
	}
	if err := se.journal.Set(&FileMetadata{
		Path:         syncRelPath,
		ETag:         createMsg.ETag,
		LocalETag:    localETag,
		Size:         createMsg.Length,
		LastModified: time.Now(),
		Version:      "",
	}); err != nil {
		se.syncStatus.SetError(syncRelPath, err)
		slog.Error("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "error", err)
		return
	}

	// mark as completed
	se.syncStatus.SetCompleted(syncRelPath)
}
