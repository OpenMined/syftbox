package sync

import (
	"log/slog"
	"path/filepath"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (se *SyncEngine) processHttpMessage(msg *syftmsg.Message) {
	httpMsg, ok := msg.Data.(*syftmsg.HttpMsg)
	if !ok {
		slog.Error("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "error", "invalid message data", "data", msg.Data)
		return
	}

	// rpc message file name
	fileName := httpMsg.Id + ".request"
	relPath := filepath.Join(httpMsg.SyftURL.ToLocalPath(), fileName)
	syncRelPath := SyncPath(relPath)

	se.syncStatus.SetSyncing(syncRelPath)
	slog.Info("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "path", httpMsg.SyftURL.ToLocalPath(), "size", len(httpMsg.Body), "etag", httpMsg.Etag)

	// rpc message file path
	rpcLocalAbsPath := se.workspace.DatasiteAbsPath(relPath)

	// a priority file was just downloaded, we don't wanna fire an event for THIS write
	se.watcher.IgnoreOnce(rpcLocalAbsPath)

	// write the RPCMsg to the file
	err := writeFileWithIntegrityCheck(
		rpcLocalAbsPath,
		httpMsg.Body,
		httpMsg.Etag,
	)

	if err != nil {
		se.syncStatus.SetError(syncRelPath, err)
		slog.Error("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "path", httpMsg.SyftURL.ToLocalPath(), "etag", httpMsg.Etag, "error", err)
		return
	}

	fileSize := int64(len(httpMsg.Body))

	// update the journal
	se.journal.Set(&FileMetadata{
		Path:         syncRelPath,
		ETag:         httpMsg.Etag,
		Size:         fileSize,
		LastModified: time.Now(),
		Version:      "",
	})

	se.syncStatus.SetCompleted(syncRelPath)

	slog.Info("sync", "type", SyncPriority, "op", OpWriteLocal, "msgType", msg.Type, "msgId", msg.Id, "path", httpMsg.SyftURL.ToLocalPath(), "size", fileSize, "etag", httpMsg.Etag)

}
