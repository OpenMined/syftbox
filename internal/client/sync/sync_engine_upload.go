package sync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/syftsdk"
)

const (
	maxUploadConcurrency = 4
)

// upload
func (se *SyncEngine) handleRemoteWrites(ctx context.Context, batch BatchRemoteWrite) {
	if len(batch) == 0 {
		return
	}

	for _, op := range batch {
		se.syncStatus.SetSyncing(op.RelPath, "standard remote write")
	}

	processUpload := func(ctx context.Context, op *SyncOperation) {
		defer se.syncStatus.SetCompleted(op.RelPath, "standard remote write")

		if op.Local.Size == 0 {
			slog.Debug("sync", "op", "SKIPPED", "reason", "empty contents", "path", op.RelPath)
			return
		}

		if changed, err := se.journal.ContentsChanged(op.RelPath, op.Local.ETag); err != nil {
			slog.Warn("journal check", "error", err)
		} else if !changed {
			slog.Debug("sync", "op", "SKIPPED", "reason", "contents unchanged", "path", op.RelPath)
			return
		}

		localAbsPath := se.workspace.DatasiteAbsPath(op.RelPath)
		res, err := se.sdk.Blob.Upload(ctx, &syftsdk.UploadParams{
			Key:      op.RelPath,
			FilePath: localAbsPath,
			// todo ChecksumCRC64NVME: op.Local.ChecksumCRC64NVME
		})
		if err != nil {
			// todo check for permission errors
			slog.Error("sync", "op", OpWriteRemote, "path", op.RelPath, "error", err)
			return
		}

		lastModified, err := time.Parse(time.RFC3339, res.LastModified)
		if err != nil {
			lastModified = time.Now()
		}
		slog.Info("sync", "op", OpWriteRemote, "path", op.RelPath)
		se.journal.Set(&FileMetadata{
			Path:         op.RelPath,
			ETag:         res.ETag,
			Size:         res.Size,
			LastModified: lastModified,
		})
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
