package sync3

import (
	"log/slog"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

const (
	maxPrioritySize = 1024 * 1024 // 1MB
)

func (se *SyncEngine) handlePriorityUpload(path string) {
	timeNow := time.Now()
	file, err := NewFileContent(path)
	if err != nil {
		slog.Error("sync priority", "op", OpWriteRemote, "error", err)
		return
	}

	if file.Size > maxPrioritySize {
		slog.Error("sync priority", "op", OpWriteRemote, "path", path, "size", file.Size, "error", "file too large")
		return
	}

	if lastSynced, err := se.journal.Get(path); err != nil {
		slog.Warn("priority file journal check", "error", err)
	} else if lastSynced.ETag == file.ETag {
		slog.Info("priority file contents unchanged. skipping.", "path", path)
		return
	}

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
