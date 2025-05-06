package sync

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

const (
	maxPrioritySize = 1024 * 1024 // 1MB
)

func (se *SyncEngine) handlePriorityUpload(path string) {
	relPath, err := se.workspace.DatasiteRelPath(path)
	if err != nil {
		slog.Error("sync priority", "op", OpWriteRemote, "error", err)
		return
	}
	se.syncStatus.SetSyncing(relPath)
	defer se.syncStatus.UnsetSyncing(relPath)

	timeNow := time.Now()
	file, err := NewFileContent(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Error("sync priority", "op", OpWriteRemote, "error", err)
		}
		return
	}

	if file.Size > maxPrioritySize {
		slog.Error("sync priority", "op", OpWriteRemote, "path", path, "size", file.Size, "error", "file too large")
		return
	}

	if lastSynced, err := se.journal.Get(relPath); err != nil {
		slog.Warn("priority file journal check", "error", err)
	} else if lastSynced != nil && lastSynced.ETag == file.ETag {
		// slog.Debug("priority file contents unchanged. skipping.", "path", path)
		return
	}

	timeTaken := timeNow.Sub(file.LastModified)
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
	metadata := &FileMetadata{
		Path:         relPath,
		ETag:         file.ETag,
		Size:         file.Size,
		LastModified: file.LastModified,
		Version:      "",
	}
	se.journal.Set(metadata)
	se.lastLocalState[relPath] = metadata
}
