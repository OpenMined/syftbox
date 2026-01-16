package sync

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

	// If we already have a rejected marker for this path, don't keep retrying until resolved.
	localAbsPath := se.workspace.DatasiteAbsPath(syncRelPath.String())
	if RejectedFileExists(localAbsPath) {
		slog.Warn("sync", "type", SyncPriority, "op", OpSkipped, "reason", "previous rejection (marker present)", "path", relPath)
		se.syncStatus.SetRejected(syncRelPath)
		se.journal.Delete(syncRelPath)
		return
	}

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
		LocalETag:    file.ETag,
		Size:         file.Size,
		LastModified: file.LastModified,
		Version:      "",
	})

	// mark as completed
	se.syncStatus.SetCompleted(syncRelPath)

	// If this was an ACL file, generate and send updated manifests
	// TODO: Re-enable after fixing timing issues
	// if aclspec.IsACLFile(relPath) {
	// 	go se.broadcastACLManifests(relPath)
	// }
}

func (se *SyncEngine) broadcastACLManifests(aclRelPath string) {
	// Extract datasite from path (e.g., "alice@example.com/public/syft.pub.yaml" -> "alice@example.com")
	parts := strings.SplitN(aclRelPath, "/", 2)
	if len(parts) == 0 {
		return
	}
	datasite := parts[0]

	// Only broadcast manifests for our own datasite
	if datasite != se.workspace.Owner {
		return
	}

	generator := NewACLManifestGenerator(se.workspace.DatasitesDir)
	manifests, err := generator.GenerateManifests(datasite)
	if err != nil {
		slog.Error("sync manifest generation failed", "datasite", datasite, "error", err)
		return
	}

	for hash, manifest := range manifests {
		msg := syftmsg.NewACLManifestMessage(manifest)
		if err := se.sdk.Events.Send(msg); err != nil {
			slog.Error("sync manifest send failed", "datasite", datasite, "forHash", hash, "error", err)
		} else {
			slog.Info("sync manifest sent", "datasite", datasite, "for", manifest.For, "forHash", hash, "aclCount", len(manifest.ACLOrder))
		}
	}
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
