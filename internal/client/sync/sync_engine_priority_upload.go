package sync

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/syftmsg"
)

const (
	maxPrioritySize = 4 * 1024 * 1024 // 4MB
)

func (se *SyncEngine) handlePriorityUpload(path string) {
	if err := se.canPrioritize(path); err != nil {
		// let standard sync handle the file
		slog.Warn("sync", "type", SyncPriority, "op", OpSkipped, "reason", err, "path", path)
		return
	}

	relPath, err := se.workspace.DatasiteRelPath(path)
	if err != nil {
		slog.Error("sync", "type", SyncPriority, "op", OpWriteRemote, "error", err)
		return
	}

	syncRelPath := SyncPath(relPath)

	// set sync status
	se.syncStatus.SetSyncing(syncRelPath)

	// get the file content
	timeNow := time.Now()
	file, err := NewFileContent(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			se.syncStatus.SetError(syncRelPath, err)
			slog.Error("sync", "type", SyncPriority, "op", OpWriteRemote, "error", err)
		} else {
			// File doesn't exist anymore, just complete silently
			se.syncStatus.SetCompleted(syncRelPath)
		}
		return
	}

	// check if the file has changed (except for ACL files, which must always broadcast)
	// ACL files bypass journal check because ACL state can cycle (owner→public→owner),
	// and when state reverts to a previous hash, journal skips upload leaving peers out of sync
	if !aclspec.IsACLFile(relPath) {
		if changed, err := se.journal.ContentsChanged(syncRelPath, file.ETag); err != nil {
			slog.Warn("sync priority journal check", "error", err)
		} else if !changed {
			slog.Debug("sync", "type", SyncPriority, "op", OpSkipped, "reason", "contents unchanged", "path", path)
			se.syncStatus.SetCompleted(syncRelPath)
			return
		}
	}

	// log the time taken to upload the file
	timeTaken := timeNow.Sub(file.LastModified)
	slog.Info("sync", "type", SyncPriority, "op", OpWriteRemote, "path", relPath, "size", file.Size, "watchLatency", timeTaken)

	// prepare the message
	message := syftmsg.NewFileWrite(
		relPath,
		file.ETag,
		file.Size,
		file.Content,
	)

	// send the message and wait for ACK/NACK (replaces 1-second sleep hack)
	ackTimeout := 5 * time.Second
	if err := se.sdk.Events.SendWithAck(message, ackTimeout); err != nil {
		se.syncStatus.SetError(syncRelPath, err)
		slog.Error("sync", "type", SyncPriority, "op", OpWriteRemote, "path", relPath, "error", err)
		return
	}

	slog.Debug("sync", "type", SyncPriority, "op", OpWriteRemote, "path", relPath, "ack", "received")

	// update the journal
	se.journal.Set(&FileMetadata{
		Path:         syncRelPath,
		ETag:         file.ETag,
		Size:         file.Size,
		LastModified: file.LastModified,
		Version:      "",
	})

	// mark as completed
	se.syncStatus.SetCompleted(syncRelPath)
}

func (se *SyncEngine) canPrioritize(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.Size() > maxPrioritySize {
		return fmt.Errorf("file too large for priority upload size=%s", humanize.Bytes(uint64(info.Size())))
	}

	return nil
}
