package sync3

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/syftsdk"
)

// upload
func (se *SyncEngine) handleRemoteWrites(ctx context.Context, batch BatchRemoteWrite) {
	if len(batch) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(batch))

	for _, op := range batch {
		go func(op *SyncOperation) {
			defer wg.Done()
			remoteRelPath := op.Path
			localFilePath := se.workspace.DatasiteAbsPath(op.Path)

			res, err := se.sdk.Blob.Upload(ctx, &syftsdk.UploadParams{
				Key:      remoteRelPath,
				FilePath: localFilePath,
				// todo ChecksumCRC64NVME: op.Local.ChecksumCRC64NVME
			})
			if err != nil {
				// todo check for permission errors
				slog.Error("sync", "op", OpWriteRemote, "path", op.Path, "error", err)
				return
			}

			lastModified, err := time.Parse(time.RFC3339, res.LastModified)
			if err != nil {
				lastModified = time.Now()
			}

			se.journal.Set(&FileMetadata{
				Path:         op.Path,
				ETag:         res.ETag,
				Size:         res.Size,
				LastModified: lastModified,
			})
			slog.Info("sync", "op", OpWriteRemote, "path", remoteRelPath)
		}(op)
	}

	wg.Wait()
}
