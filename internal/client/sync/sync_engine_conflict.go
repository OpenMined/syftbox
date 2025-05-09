package sync

import (
	"context"
	"log/slog"
)

func (se *SyncEngine) handleConflicts(_ context.Context, batch BatchConflict) {
	for _, op := range batch {
		se.syncStatus.SetSyncing(op.RelPath, "conflict")
		defer se.syncStatus.SetCompleted(op.RelPath, "conflict")

		localPath := se.workspace.DatasiteAbsPath(op.RelPath)
		if err := markConflicted(localPath); err != nil {
			slog.Warn("sync", "op", OpConflict, "key", op.RelPath, "error", err)
		} else {
			slog.Warn("sync", "op", OpConflict, "key", op.RelPath)
		}
		// todo trigger a re-download, or wait for the next sync cycle to re-download
	}
}
