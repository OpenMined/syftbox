package sync3

import (
	"context"
	"log/slog"
)

func (se *SyncEngine) handleConflicts(_ context.Context, batch BatchConflict) {
	for _, op := range batch {
		if err := MarkConflicted(op.Local.Path); err != nil {
			slog.Warn("sync", "op", OpConflict, "key", op.Local.Path, "error", err)
		} else {
			slog.Warn("sync", "op", OpConflict, "key", op.Local.Path)
		}
		// todo trigger a re-download, or wait for the next sync cycle to re-download
	}
}
