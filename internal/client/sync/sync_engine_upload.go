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

	"github.com/dustin/go-humanize"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
)

var (
	maxUploadConcurrency = 8
	uploadSessionTTL     = 24 * time.Hour
)

// upload
func (se *SyncEngine) handleRemoteWrites(ctx context.Context, batch BatchRemoteWrite) {
	if len(batch) == 0 {
		return
	}

	// set all files in syncing state
	for _, op := range batch {
		se.syncStatus.SetSyncing(op.RelPath)
	}

	processUpload := func(ctx context.Context, op *SyncOperation) {
		if op.Local.Size == 0 {
			slog.Debug("sync", "type", SyncStandard, "op", OpSkipped, "reason", "empty contents", "path", op.RelPath)
			se.syncStatus.SetCompleted(op.RelPath)
			return
		}

		if changed, err := se.journal.ContentsChanged(op.RelPath, op.Local.ETag); err != nil {
			slog.Warn("journal check", "error", err)
		} else if !changed {
			slog.Debug("sync", "type", SyncStandard, "op", OpSkipped, "reason", "contents unchanged", "path", op.RelPath)
			se.syncStatus.SetCompleted(op.RelPath)
			return
		}

		localAbsPath := se.workspace.DatasiteAbsPath(op.RelPath.String())
		if !utils.FileExists(localAbsPath) {
			slog.Debug("sync", "type", SyncStandard, "op", OpSkipped, "reason", "file no longer exists", "path", op.RelPath)
			se.syncStatus.SetCompleted(op.RelPath)
			return
		}

		// Skip files that don't belong to the current user - these are downloaded from other datasites
		// and should not be re-uploaded
		if !se.workspace.IsOwner(op.RelPath.String()) {
			slog.Debug("sync", "type", SyncStandard, "op", OpSkipped, "reason", "not owner", "path", op.RelPath)
			se.syncStatus.SetCompleted(op.RelPath)
			return
		}

		if !se.workspace.IsValidPath(op.RelPath.String()) {
			slog.Error("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "error", "invalid datasite path", "DEBUG_REJECTION_REASON", "IsValidPath_check_failed")
			markedPath, markErr := SetMarker(localAbsPath, Rejected)
			if markErr != nil {
				slog.Error("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "error", markErr)
				se.syncStatus.SetError(op.RelPath, markErr)
			} else {
				slog.Warn("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "movedTo", markedPath, "DEBUG_REJECTION_REASON", "IsValidPath_check_failed")
				se.syncStatus.SetRejected(op.RelPath)
			}
			se.journal.Delete(op.RelPath)
			return
		}

		resumeDir := filepath.Join(se.workspace.MetadataDir, "upload-sessions")
		cleanupOldUploadSessions(resumeDir, uploadSessionTTL)

		uploadInfo, uploadCtx, cancel := se.uploadRegistry.Register(op.RelPath.String(), localAbsPath, op.Local.Size)
		defer cancel()

		res, err := se.sdk.Blob.Upload(uploadCtx, &syftsdk.UploadParams{
			Key:         op.RelPath.String(),
			FilePath:    localAbsPath,
			Fingerprint: op.Local.ETag,
			ResumeDir:   resumeDir,
			Callback: func(uploadedBytes int64, totalBytes int64) {
				progress := float64(uploadedBytes) / float64(totalBytes)
				se.syncStatus.SetProgress(op.RelPath, progress)
				se.uploadRegistry.UpdateProgress(
					uploadInfo.ID,
					uploadedBytes,
					nil,
					0,
					0,
				)
				slog.Debug("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "progress", fmt.Sprintf("%.2f%%", progress*100.0))
			},
		})

		if err != nil {
			var sdkErr syftsdk.SDKError
			if errors.As(err, &sdkErr) {
				switch sdkErr.ErrorCode() {
				case syftsdk.CodeAccessDenied, syftsdk.CodeDatasiteInvalidPath:
					// not allowed to upload/write the file.
					// 1. mark as rejected
					// 2. delete from journal
					// 3. need to pull the previous version again
					if markedPath, markErr := SetMarker(localAbsPath, Rejected); markErr != nil {
						// Failed to mark as rejected, set error state
						se.syncStatus.SetError(op.RelPath, markErr)
						slog.Error("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "error", markErr, "DEBUG_REJECTION_REASON", "SetMarker_failed")
					} else {
						// Successfully marked as rejected
						slog.Error("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "error", sdkErr, "movedTo", markedPath, "DEBUG_REJECTION_REASON", fmt.Sprintf("server_error_code_%s", sdkErr.ErrorCode()), "DEBUG_SERVER_ERROR", sdkErr.Error())
						se.syncStatus.SetRejected(op.RelPath)
					}
					se.journal.Delete(op.RelPath)
				default:
					// this can be http timeouts or other retryable errors
					se.syncStatus.SetError(op.RelPath, sdkErr)
					slog.Error("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "error", sdkErr)
				}
			} else {
				se.syncStatus.SetError(op.RelPath, err)
				slog.Error("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "error", err)
			}
			se.uploadRegistry.SetError(uploadInfo.ID, err)
			return // return on ANY error
		}
		if errors.Is(uploadCtx.Err(), context.Canceled) {
			se.uploadRegistry.SetError(uploadInfo.ID, errors.New("upload cancelled"))
			return
		}

		lastModified, err := time.Parse(time.RFC3339, res.LastModified)
		if err != nil {
			lastModified = time.Now()
		}
		slog.Info("sync", "type", SyncStandard, "op", OpWriteRemote, "path", op.RelPath, "size", humanize.Bytes(uint64(res.Size)))
		se.journal.Set(&FileMetadata{
			Path:         op.RelPath,
			ETag:         res.ETag,
			Size:         res.Size,
			LastModified: lastModified,
		})

		// mark as completed on success
		se.syncStatus.SetCompleted(op.RelPath)
		se.uploadRegistry.SetCompleted(uploadInfo.ID)
	}

	var wg sync.WaitGroup
	opsChan := make(chan *SyncOperation, len(batch))

	// Start worker goroutines
	wg.Add(maxUploadConcurrency)
	for range maxUploadConcurrency {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return // Context cancelled
				case op, ok := <-opsChan:
					if !ok {
						return // Channel closed
					}
					processUpload(ctx, op)
				}
			}
		}()
	}

	// Send operations to the channel
	for _, op := range batch {
		opsChan <- op
	}
	close(opsChan) // Close channel to signal no more operations

	// Wait for all worker goroutines to finish processing
	wg.Wait()
}

// cleanupOldUploadSessions trims stale resumable upload state so disk doesn't accumulate abandoned sessions.
func cleanupOldUploadSessions(dir string, ttl time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("cleanup upload sessions", "dir", dir, "error", err)
		}
		return
	}

	cutoff := time.Now().Add(-ttl)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
