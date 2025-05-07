package sync

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/syftsdk"
)

func (se *SyncEngine) handleLocalDeletes(_ context.Context, batch BatchLocalDelete) {
	uniqueParents := make(map[string]struct{})

	if len(batch) == 0 {
		return
	}

	for _, op := range batch {
		// get the local path
		localPath := se.workspace.DatasiteAbsPath(op.RelPath)

		// set sync status
		se.syncStatus.SetSyncing(op.RelPath, "standard local delete")

		// delete the file
		if err := os.Remove(localPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				se.syncStatus.SetError(op.RelPath, "standard local delete")
				slog.Warn("sync", "op", OpDeleteLocal, "path", localPath, "error", err)
				continue
			} else {
				slog.Debug("sync", "op", OpDeleteLocal, "path", op.RelPath, "message", "file was already deleted")
			}
		}

		// commit delete to journal
		se.journal.Delete(op.RelPath)

		// set sync status
		se.syncStatus.SetCompleted(op.RelPath, "standard local delete")
		slog.Info("sync", "op", OpDeleteLocal, "path", op.RelPath)

		// add to unique parents
		uniqueParents[filepath.Dir(localPath)] = struct{}{}
	}

	// cleanup empty parent directories
	for parent := range uniqueParents {
		cleanupEmptyParentDirs(parent, se.workspace.DatasitesDir)
	}
}

func (se *SyncEngine) handleRemoteDeletes(ctx context.Context, batch BatchRemoteDelete) {
	if len(batch) == 0 {
		return
	}

	keysToDelete := make([]string, 0, len(batch))
	for _, op := range batch {
		// set sync status
		se.syncStatus.SetSyncing(op.RelPath, "standard remote delete")

		// add to keys to delete
		keysToDelete = append(keysToDelete, op.RelPath)
	}

	// delete the files
	result, err := se.sdk.Blob.Delete(ctx, &syftsdk.DeleteParams{
		Keys: keysToDelete,
	})
	if err != nil {
		slog.Error("sync", "op", OpDeleteRemote, "http error", err)
		return
	}

	for _, key := range result.Deleted {
		// commit delete to journal
		se.journal.Delete(key)

		// set sync status
		se.syncStatus.SetCompleted(key, "standard remote delete")
		slog.Info("sync", "op", OpDeleteRemote, "path", key)
	}

	for _, err := range result.Errors {
		// set sync status
		se.syncStatus.SetError(err.Key, "standard remote delete")
		slog.Warn("sync", "op", OpDeleteRemote, "path", err.Key, "error", err.Error)
	}
}
func cleanupEmptyParentDirs(initialDirToCleanup string, workspaceRoot string) {
	currentDir := initialDirToCleanup

	for {
		// Safety checks for currentDir:
		if currentDir == workspaceRoot || !strings.HasPrefix(currentDir, workspaceRoot) || currentDir == filepath.Dir(currentDir) {
			break // Stop: reached workspace/filesystem boundary or root itself
		}

		// Stat the directory to ensure it exists and is a directory.
		if statInfo, statErr := os.Stat(currentDir); errors.Is(statErr, os.ErrNotExist) || !statInfo.IsDir() {
			break
		}

		// Directory exists and is a directory, check if it's empty.
		dirEntries, err := os.ReadDir(currentDir)
		if err != nil {
			slog.Warn("sync", "op", OpDeleteLocal, "path", currentDir, "error", err)
			break
		}

		if len(dirEntries) > 0 {
			// Directory is not empty. Stop the cleanup for this particular upward chain.
			break
		}

		// Directory is empty, try to remove it.
		err = os.Remove(currentDir)
		if err != nil {
			slog.Warn("sync", "op", OpDeleteLocal, "path", currentDir, "error", err)
			break
		} else {
			// Successfully removed the directory.
			// Matching your requested debug log for successful removal.
			slog.Info("sync", "op", OpDeleteLocal, "path", currentDir, "reason", "empty parent dir")
			// Move to the parent directory for the next iteration.
			currentDir = filepath.Dir(currentDir)
		}
	}
}
