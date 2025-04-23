package sync3

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openmined/syftbox/internal/queue"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
)

// download
func (se *SyncEngine) handleLocalWrites(ctx context.Context, batch BatchLocalWrite) {
	if len(batch) == 0 {
		return
	}

	downloader, err := syftsdk.NewDownloader(4)
	if err != nil {
		slog.Error("sync", "op", OpWriteLocal, "error", err)
		return
	}
	defer downloader.Stop()

	// group by etag
	uniqueFiles := make(map[string]string)
	etagMap := make(map[string][]string)
	pathMap := make(map[string]*FileMetadata)
	for _, op := range batch {
		uniqueFiles[op.Remote.ETag] = op.Path
		etagMap[op.Remote.ETag] = append(etagMap[op.Remote.ETag], op.Path)
		pathMap[op.Path] = op.Remote
	}

	// get presigned urls
	uniquePaths := slices.Collect(maps.Values(uniqueFiles))
	resUrls, err := se.sdk.Blob.DownloadPresigned(ctx, &syftsdk.PresignedParams{
		Keys: uniquePaths,
	})
	if err != nil {
		slog.Error("sync", "op", OpWriteLocal, "http error", err)
		// todo set status = ERROR for all files
		return
	}
	for _, url := range resUrls.Errors {
		slog.Warn("sync", "op", OpWriteLocal, "path", url.Key, "error", url.Error)
		// todo set status = SYNCED
	}

	// build priority queue
	pq := queue.NewPriorityQueue[*syftsdk.DownloadJob]()
	for _, url := range resUrls.URLs {
		meta := pathMap[url.Key]
		priority := se.getFilePriority(meta)

		pq.Enqueue(&syftsdk.DownloadJob{
			URL:      url.URL,
			FileName: meta.ETag,
		}, priority)
	}

	// now dequeue all as list
	orderedJobs := pq.DequeueAll()
	resChan := downloader.DownloadAll(ctx, orderedJobs)
	for {
		select {
		case <-ctx.Done():
			slog.Warn("context cancelled")
			return
		case res, ok := <-resChan:
			if !ok {
				return
			}

			if res.Error != nil {
				slog.Warn("failed to download file", "url", res.URL, "etag", res.FileName, "error", res.Error)
				// todo set status = ERROR for all files
				continue
			}

			etagToCopy, exists := etagMap[res.FileName]
			if !exists || len(etagToCopy) == 0 {
				slog.Warn("no keys found for downloaded file", "name", res.FileName)
				continue
			}

			for _, key := range etagToCopy {
				targetPath := filepath.Join(se.workspace.DatasitesDir, key)
				err := copyLocal(res.DownloadPath, targetPath)
				if err != nil {
					slog.Error("downloaded but failed to copy file", "from", res.DownloadPath, "to", targetPath, "error", err)
				} else {
					meta := pathMap[key]
					se.journal.Set(meta)
					slog.Info("sync", "op", OpWriteLocal, "path", key, "size", meta.Size)
				}
			}
		}
	}
}

func (se *SyncEngine) getFilePriority(meta *FileMetadata) int {
	// file size + key length
	priority := int(meta.Size) + len(meta.Path)

	// user's datasite should be downloaded first
	if strings.HasPrefix(meta.Path, se.workspace.Owner) {
		priority = 0
	} else if strings.HasSuffix(meta.Path, "syft.pub.yaml") {
		// syftpub goes brr is important
		priority = 1
	} else if strings.Contains(meta.Path, "/rpc/") {
		// rpc is important
		priority = 2
	}

	return priority
}

func copyLocal(src, dst string) error {
	if err := utils.EnsureParent(dst); err != nil {
		return err
	}

	// Open the source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create the destination file
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy the contents
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Flush contents to disk
	// err = destFile.Sync()
	// if err != nil {
	// 	return err
	// }

	return nil
}
