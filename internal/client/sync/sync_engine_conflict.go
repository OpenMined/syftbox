package sync

import (
	"context"
	"log/slog"
)

func (se *SyncEngine) handleConflicts(_ context.Context, batch BatchConflict) {
	for _, op := range batch {
		// set the file in syncing state
		se.syncStatus.SetSyncing(op.RelPath)

		// get local absolute path of the file
		localPath := se.workspace.DatasiteAbsPath(op.RelPath.String())

		// mark the file as conflicted
		if markedPath, err := SetMarker(localPath, Conflict); err != nil {
			// this can fail due to several reasons:
			// 1. os errors (e.g. permission denied, file not found, disk full, cross-device link, file locked, invalid path)
			// 2. rotateByModTime failure - when a conflict file already exists and rotation fails due to similar reasons (permissions, disk space, etc.)
			// In all these cases, we couldn't mark the file as conflicted
			// it will remain in error state until the user manually fixes this
			slog.Error("sync", "type", SyncStandard, "op", OpConflict, "key", op.RelPath, "error", err)
			se.syncStatus.SetError(op.RelPath, err)
		} else {
			// successfully marked as conflicted
			// set both sync status to completed (the conflict marking operation is done)
			// and file status to conflicted (the file has conflict files)
			slog.Warn("sync", "type", SyncStandard, "op", OpConflict, "key", op.RelPath, "movedTo", markedPath)
			se.syncStatus.SetConflicted(op.RelPath)
		}

		// todo rollback to the previous version of the file
	}
}
