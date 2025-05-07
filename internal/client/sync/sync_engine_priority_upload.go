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

	// set sync status
	se.syncStatus.SetSyncing(relPath, "priority upload")
	defer se.syncStatus.SetCompleted(relPath, "priority upload")

	// get the file content
	timeNow := time.Now()
	file, err := NewFileContent(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Error("sync priority", "op", OpWriteRemote, "error", err)
		}
		return
	}

	// check file size
	if file.Size > maxPrioritySize {
		slog.Error("sync priority", "op", OpWriteRemote, "path", path, "size", file.Size, "error", "file too large")
		return
	}

	// check if the file has changed
	if changed, err := se.journal.ContentsChanged(relPath, file.ETag); err != nil {
		slog.Warn("sync priority journal check", "error", err)
	} else if !changed {
		slog.Debug("sync priority", "op", "SKIPPED", "reason", "contents unchanged", "path", path)
		return
	}

	// log the time taken to upload the file
	timeTaken := timeNow.Sub(file.LastModified)
	slog.Info("sync priority", "op", OpWriteRemote, "path", relPath, "size", file.Size, "watchLatency", timeTaken)

	// prepare the message
	message := syftmsg.NewFileWrite(
		relPath,
		file.ETag,
		file.Size,
		file.Content,
	)

	// send the message
	if err := se.sdk.Events.Send(message); err != nil {
		slog.Error("sync priority", "op", OpWriteRemote, "path", relPath, "error", err)
	}

	// update the journal
	se.journal.Set(&FileMetadata{
		Path:         relPath,
		ETag:         file.ETag,
		Size:         file.Size,
		LastModified: file.LastModified,
		Version:      "",
	})
}
