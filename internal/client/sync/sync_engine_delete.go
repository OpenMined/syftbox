package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/syftsdk"
)

const (
	deleteBatchSize = 50
)

func (se *SyncEngine) handleLocalDeletes(_ context.Context, batch BatchLocalDelete) {
	uniqueParents := make(map[string]struct{})

	if len(batch) == 0 {
		return
	}

	for _, op := range batch {
		// get the local path
		localPath := se.workspace.DatasiteAbsPath(op.RelPath.String())

		// set sync status
		se.syncStatus.SetSyncing(op.RelPath)

		// delete the file
		err := os.Remove(localPath)
		switch {
		case err == nil:
			// file was deleted successfully
			se.syncStatus.SetCompletedAndRemove(op.RelPath)
			slog.Info("sync", "type", SyncStandard, "op", OpDeleteLocal, "path", op.RelPath)
		case errors.Is(err, os.ErrNotExist):
			// file does not exist locally, so it's fine
			// just log and continue to be evicted from journal
			se.syncStatus.SetCompletedAndRemove(op.RelPath)
			slog.Debug("sync", "type", SyncStandard, "op", OpDeleteLocal, "path", op.RelPath, "message", "file was already deleted")
		default:
			// rare case - can be permission error, or file is locked by another process, or whatever
			// THIS WILL REQUIRE A HUMAN INTERVENTION, as it will keep complaining every sync cycle
			// for now we'll just log and continue to be evicted from journal
			err = fmt.Errorf("failed to delete file: %w", err)
			se.syncStatus.SetError(op.RelPath, err)
			slog.Error("sync", "type", SyncStandard, "op", OpDeleteLocal, "path", localPath, "error", err)
		}

		// commit delete to journal
		se.journal.Delete(op.RelPath)

		// add to unique parents for cleanup
		uniqueParents[filepath.Dir(localPath)] = struct{}{}
	}

	// cleanup empty parent directories
	for parent := range uniqueParents {
		cleanupEmptyParentDirs(parent, se.workspace.DatasitesDir)
	}
}

// deleteResult holds either a successful delete response or an error
type deleteResult struct {
	response *syftsdk.DeleteResponse
	err      error
	batch    []*SyncOperation
}

func (se *SyncEngine) handleRemoteDeletes(ctx context.Context, batch BatchRemoteDelete) {
	if len(batch) == 0 {
		return
	}

	// Set sync status to syncing for all files
	for _, op := range batch {
		se.syncStatus.SetSyncing(op.RelPath)
	}

	if len(batch) <= deleteBatchSize {
		// If batch size <= 10, use the original single API call approach
		se.remoteDelete(ctx, batch)
	} else {
		// For large batches, use worker pools with batch size of 50
		se.remoteDeleteBatched(ctx, batch, deleteBatchSize)
	}
}

func (se *SyncEngine) remoteDelete(ctx context.Context, batch BatchRemoteDelete) {
	keysToDelete := make([]string, 0, len(batch))
	batchVals := make([]*SyncOperation, 0, len(batch))
	for _, op := range batch {
		keysToDelete = append(keysToDelete, op.RelPath.String())
		batchVals = append(batchVals, op)
	}

	// delete the files from the server
	result, err := se.sdk.Blob.Delete(ctx, &syftsdk.DeleteParams{
		Keys: keysToDelete,
	})

	// handle the result
	se.handleDeleteResult(&deleteResult{
		response: result,
		err:      err,
		batch:    batchVals,
	})
}

func (se *SyncEngine) remoteDeleteBatched(ctx context.Context, batch BatchRemoteDelete, batchSize int) {
	// Convert map to slice for chunking
	batchVals := make([]*SyncOperation, 0, len(batch))
	for _, op := range batch {
		batchVals = append(batchVals, op)
	}

	// Split batch into batchChunks
	batchChunks := se.chunkBatchSlice(batchVals, batchSize)

	// Create unified result channel
	resultChan := make(chan *deleteResult, len(batchChunks))

	// Start workers (max 8)
	numWorkers := min(len(batchChunks), 8)

	var wg sync.WaitGroup

	// Create work channel
	workChan := make(chan []*SyncOperation, len(batchChunks))

	// Start result processor
	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		for result := range resultChan {
			se.handleDeleteResult(result)
		}
	}()

	// Start workers
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range workChan {
				keysToDelete := make([]string, 0, len(chunk))
				for _, op := range chunk {
					keysToDelete = append(keysToDelete, op.RelPath.String())
				}

				result, err := se.sdk.Blob.Delete(ctx, &syftsdk.DeleteParams{
					Keys: keysToDelete,
				})

				resultChan <- &deleteResult{
					response: result,
					err:      err,
					batch:    chunk,
				}
			}
		}()
	}

	// Send work to workers
	for _, chunk := range batchChunks {
		workChan <- chunk
	}
	close(workChan)

	// Wait for all workers to complete
	wg.Wait()
	close(resultChan)

	// Wait for result processor to complete
	resultWg.Wait()
}

func (se *SyncEngine) chunkBatchSlice(batchSlice []*SyncOperation, chunkSize int) [][]*SyncOperation {
	var chunks [][]*SyncOperation
	for i := 0; i < len(batchSlice); i += chunkSize {
		end := min(i+chunkSize, len(batchSlice))
		chunks = append(chunks, batchSlice[i:end])
	}
	return chunks
}

func (se *SyncEngine) handleDeleteResult(result *deleteResult) {
	if result.err != nil {
		// entire batch operation failed - this is usually bad auth, server death, bad server upgrade, or something else
		// The subsequent sync cycle(s) should fix it.
		// So mark all as ERROR, but DO NOT REMOVE from the journal, because:
		//   1. local & remote states have diverged.
		//   2. re-download will confuse the user
		for _, op := range result.batch {
			se.syncStatus.SetError(op.RelPath, fmt.Errorf("failed to delete file: %w", result.err))
		}
		slog.Error("sync", "type", SyncStandard, "op", OpDeleteRemote, "keys", len(result.batch), "error", result.err)
		// arr cap'n - on towards the next sync cycle
		return
	}

	// handle successful deletes
	for _, key := range result.response.Deleted {
		// commit successful delete to journal
		syncRelPath := SyncPath(key)
		se.journal.Delete(syncRelPath)
		se.syncStatus.SetCompletedAndRemove(syncRelPath)
		slog.Info("sync", "type", SyncStandard, "op", OpDeleteRemote, "path", key)
	}

	// handle errors
	for _, err := range result.response.Errors {
		syncRelPath := SyncPath(err.Key)
		switch err.ErrorCode() {
		case syftsdk.CodeAccessDenied:
			// tryna be smart now are we?
			// user deleted a file for which they do not have permissions for
			// just mark as ERROR & REMOVE from journal to redownload (ideally we'd want to download right away)
			se.syncStatus.SetError(syncRelPath, fmt.Errorf("failed to delete file: %w", err))
			se.journal.Delete(syncRelPath)
			slog.Error("sync", "type", SyncStandard, "op", OpDeleteRemote, "path", syncRelPath, "error", err)
		case syftsdk.CodeBlobDeleteFailed:
			// server's internal op got rekt, probably because S3 died or something
			// this is similar to entire batch operation failing
			// just mark as ERROR, DO NOT REMOVE from the journal, let next sync cycle handle it
			se.syncStatus.SetError(syncRelPath, fmt.Errorf("failed to delete file: %w", err))
			slog.Warn("sync", "type", SyncStandard, "op", OpDeleteRemote, "path", syncRelPath, "error", err)
		default:
			// covers CodeDatasiteInvalidPath
			// just mark as COMPLETED & REMOVE from journal
			se.journal.Delete(syncRelPath)
			se.syncStatus.SetCompletedAndRemove(syncRelPath)
			slog.Info("sync", "type", SyncStandard, "op", OpDeleteRemote, "path", syncRelPath)
		}
	}
}

func cleanupEmptyParentDirs(initialDirToCleanup string, workspaceRoot string) {
	currentDir := initialDirToCleanup

	for {
		if currentDir == workspaceRoot {
			break
		}

		if statInfo, statErr := os.Stat(currentDir); errors.Is(statErr, os.ErrNotExist) || !statInfo.IsDir() {
			break
		}

		dirEntries, err := os.ReadDir(currentDir)
		if err != nil {
			slog.Warn("sync", "type", SyncStandard, "op", OpDeleteLocal, "path", currentDir, "error", err)
			break
		}

		remaining := 0
		for _, entry := range dirEntries {
			if entry.Name() == ".DS_Store" || entry.Name() == "Thumbs.db" {
				_ = os.RemoveAll(filepath.Join(currentDir, entry.Name()))
			} else {
				remaining++
			}
		}

		if remaining > 0 {
			break
		}

		// Directory is empty (or only had garbage files), try to remove it.
		// Retry briefly on failure — Windows can hold handles after file deletion.
		var rmErr error
		for attempt := 0; attempt < 3; attempt++ {
			if rmErr = os.Remove(currentDir); rmErr == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if rmErr != nil {
			slog.Warn("sync", "type", SyncStandard, "op", OpDeleteLocal, "path", currentDir, "error", rmErr)
			break
		}
		slog.Info("sync", "type", SyncStandard, "op", "Cleanup", "path", currentDir, "reason", "empty parent dir")
		currentDir = filepath.Dir(currentDir)
	}
}
