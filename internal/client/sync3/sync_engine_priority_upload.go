package sync3

import (
	"log/slog"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func (se *SyncEngine) handlePriorityUpload(path string) {
	timeNow := time.Now()
	file, err := ReadPriorityFile(path)
	if err != nil {
		slog.Error("sync priority", "op", OpWriteRemote, "error", err)
		return
	}

	if file.Size > maxPrioritySize {
		slog.Error("sync priority", "op", OpWriteRemote, "path", path, "size", file.Size, "error", "file too large")
		return
	}

	// todo - if file is unchanged, just random events, then skip
	// if se.journal.HasETag(file.ETag) {
	// 	slog.Warn("priority file unchanged", "path", path)
	// 	return
	// }

	timeTaken := timeNow.Sub(file.LastModified)
	relPath := se.workspace.DatasiteRelPath(path)
	slog.Info("sync priority", "op", OpWriteRemote, "path", relPath, "size", file.Size, "watchLatency", timeTaken)

	message := syftmsg.NewFileWrite(
		relPath,
		file.ETag,
		file.Size,
		file.Content,
	)

	if err := se.sdk.Events.Send(message); err != nil {
		slog.Error("sync priority", "op", OpWriteRemote, "path", relPath, "error", err)
	}

	// update journal and lastLocalState
	se.journal.Set(&file.FileMetadata)
	se.lastLocalState[relPath] = &file.FileMetadata
}
