package sync

import (
	"context"
	"log/slog"
	"os"

	"github.com/openmined/syftbox/internal/syftsdk"
)

func (se *SyncEngine) handleLocalDeletes(_ context.Context, batch BatchLocalDelete) {
	if len(batch) == 0 {
		return
	}

	for _, op := range batch {
		se.syncStatus.SetSyncing(op.RelPath)

		localPath := se.workspace.DatasiteAbsPath(op.RelPath)
		if err := os.Remove(localPath); err != nil {
			// todo set status = ERROR
			slog.Warn("sync", "op", OpDeleteLocal, "path", localPath, "error", err)
			continue
		}
		// todo set status = SYNCED

		// commit to journal
		se.journal.Delete(localPath)
		slog.Info("sync", "op", OpDeleteLocal, "path", localPath)
		se.syncStatus.UnsetSyncing(op.RelPath)
	}
}

func (se *SyncEngine) handleRemoteDeletes(ctx context.Context, batch BatchRemoteDelete) {
	if len(batch) == 0 {
		return
	}

	keysToDelete := make([]string, 0, len(batch))
	for _, op := range batch {
		se.syncStatus.SetSyncing(op.RelPath)
		keysToDelete = append(keysToDelete, op.RelPath)
	}

	result, err := se.sdk.Blob.Delete(ctx, &syftsdk.DeleteParams{
		Keys: keysToDelete,
	})
	if err != nil {
		slog.Error("sync", "op", OpDeleteRemote, "http error", err)
		return
	}

	for _, key := range result.Deleted {
		// todo set status = SYNCED
		se.syncStatus.UnsetSyncing(key)
		// commit to journal
		se.journal.Delete(key)
		slog.Info("sync", "op", OpDeleteRemote, "path", key)
	}

	for _, err := range result.Errors {
		// todo set status = ERROR
		se.syncStatus.UnsetSyncing(err.Key)
		slog.Warn("sync", "op", OpDeleteRemote, "path", err.Key, "error", err.Error)
	}
}
