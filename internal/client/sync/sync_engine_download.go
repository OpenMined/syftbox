package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/openmined/syftbox/internal/queue"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	downloadBatchSize = 100
)

// These indirections allow tests to simulate platform-specific rename behavior.
var (
	renameFile  = os.Rename
	runtimeGOOS = runtime.GOOS
)

// downloadResult represents the outcome of a single file download operation.
type downloadResult struct {
	Path     string
	Metadata *FileMetadata
	Error    error
}

// pendingDownload represents a file waiting to be downloaded.
type pendingDownload struct {
	ETag     string
	RelPath  string
	Metadata *FileMetadata
}

// handleLocalWrites orchestrates the download of a batch of files.
// It sets the initial syncing status and then processes results as they are streamed
// from the downloadBatch helper, updating the journal and sync status accordingly.
func (se *SyncEngine) handleLocalWrites(ctx context.Context, batch BatchLocalWrite) {
	if len(batch) == 0 {
		return
	}

	// Immediately set the status for all files in the batch to "syncing".
	for _, op := range batch {
		se.syncStatus.SetSyncing(op.RelPath)
	}

	// Start the download process and get a channel for the results.
	results, err := se.downloadBatchUnique(ctx, batch)
	if err != nil {
		slog.Error("sync", "type", SyncStandard, "op", OpWriteLocal, "status", "Failed", "error", err)
		return
	}

	// Process each result as it becomes available.
	for res := range results {
		syncRelPath := SyncPath(res.Path)
		if res.Error != nil {
			var sdkErr syftsdk.SDKError
			if errors.As(res.Error, &sdkErr) && strings.HasPrefix(sdkErr.ErrorCode(), syftsdk.CodePresignedURLErrors) {
				slog.Warn("sync", "type", SyncStandard, "op", OpWriteLocal, "status", "Ignored", "path", res.Path, "error", sdkErr)
				se.syncStatus.SetCompletedAndRemove(syncRelPath)
			} else {
				slog.Error("sync error", "type", SyncStandard, "op", OpWriteLocal, "status", "Error", "path", res.Path, "error", res.Error)
				se.syncStatus.SetError(syncRelPath, res.Error)
			}
			continue
		}

		se.journal.Set(res.Metadata)
		se.syncStatus.SetCompleted(syncRelPath)
		slog.Info("sync", "type", SyncStandard, "op", OpWriteLocal, "status", "Completed", "path", res.Path, "size", humanize.Bytes(uint64(res.Metadata.Size)))
	}
}

// downloadBatchUnique handles the core logic of downloading a batch of files.
// It deduplicates files by ETag, fetches presigned URLs in chunks of 100, prioritizes downloads,
// and executes them. It runs in a goroutine and streams results back over a channel.
func (se *SyncEngine) downloadBatchUnique(ctx context.Context, batch BatchLocalWrite) (<-chan downloadResult, error) {
	resultsChan := make(chan downloadResult, len(batch))

	tempDir, err := os.MkdirTemp("", "syftbox-blobs-*")
	if err != nil {
		return nil, err
	}

	go func() {
		defer func() {
			close(resultsChan)
			os.RemoveAll(tempDir)
		}()

		// Group files by ETag to avoid downloading the same content multiple times.
		uniqueFiles := make(map[string]string)       // ETag -> RelPath
		etagToPaths := make(map[string][]string)     // ETag -> All Paths with this content
		pathToMeta := make(map[string]*FileMetadata) // Path -> Metadata
		for _, op := range batch {
			syncRelPath := op.RelPath.String()
			uniqueFiles[op.Remote.ETag] = syncRelPath
			etagToPaths[op.Remote.ETag] = append(etagToPaths[op.Remote.ETag], syncRelPath)
			pathToMeta[syncRelPath] = op.Remote
		}

		// Build priority queue with all unique files (no URLs yet).
		pq := queue.NewPriorityQueue[*pendingDownload]()
		for etag, relPath := range uniqueFiles {
			meta := pathToMeta[relPath]
			priority := se.getDownloadPriority(meta)
			pq.Enqueue(&pendingDownload{
				ETag:     etag,
				RelPath:  relPath,
				Metadata: meta,
			}, priority)
		}

		// Process downloads in batches to avoid URL expiration.
		for pq.Len() > 0 {
			// Get next chunk of files to download.
			currentChunkSize := min(downloadBatchSize, pq.Len())
			chunkPaths := make([]string, 0, currentChunkSize)
			chunkItems := make([]*pendingDownload, 0, currentChunkSize)

			for range currentChunkSize {
				item, _ := pq.Dequeue()
				chunkPaths = append(chunkPaths, item.RelPath)
				chunkItems = append(chunkItems, item)
			}

			// Get presigned URLs for this chunk.
			resUrls, err := se.sdk.Blob.Download(ctx, &syftsdk.PresignedParams{
				Keys: chunkPaths,
			})
			if err != nil {
				// On total failure, send an error for every file in this chunk.
				for _, item := range chunkItems {
					for _, path := range etagToPaths[item.ETag] {
						resultsChan <- downloadResult{Path: path, Metadata: pathToMeta[path], Error: err}
					}
				}
				continue
			}

			// Handle errors for individual URL generations.
			dlJobs := make([]*syftsdk.DownloadJob, 0, len(resUrls.URLs))
			for _, urlErr := range resUrls.Errors {
				meta := pathToMeta[urlErr.Key]
				for _, path := range etagToPaths[meta.ETag] {
					resultsChan <- downloadResult{Path: path, Metadata: pathToMeta[path], Error: urlErr}
				}
			}

			// Build download jobs for successful URLs.
			for _, url := range resUrls.URLs {
				meta := pathToMeta[url.Key]
				dlJobs = append(dlJobs, &syftsdk.DownloadJob{
					URL:       url.URL,
					TargetDir: tempDir,
					Name:      meta.ETag, // Use ETag as the unique identifier for the download content.
					Callback: func(job *syftsdk.DownloadJob, downloadedBytes int64, totalBytes int64) {
						key := url.Key
						// ignore small files
						if totalBytes < 4*1024*1024 {
							return
						}
						progress := float64(downloadedBytes) / float64(totalBytes) * 100.0
						se.syncStatus.SetProgress(SyncPath(key), progress)
						slog.Debug("sync", "type", SyncStandard, "op", OpWriteLocal, "status", "Downloading", "path", key, "progress", fmt.Sprintf("%.2f%%", progress))
					},
				})
			}

			// Skip if no valid jobs in this chunk.
			if len(dlJobs) == 0 {
				continue
			}

			// Download this chunk and process results.
			downloadResultsChan := syftsdk.Downloader(ctx, &syftsdk.DownloadOpts{
				Workers: 8,
				Jobs:    dlJobs,
			})
			for res := range downloadResultsChan {
				etag := res.Name // res.Name is the ETag
				pathsToCopy, exists := etagToPaths[etag]
				if !exists {
					continue // ??? unlikely
				}

				// Handle download failure.
				if res.Error != nil {
					for _, p := range pathsToCopy {
						resultsChan <- downloadResult{Path: p, Metadata: pathToMeta[p], Error: res.Error}
					}
					continue
				}

				// Handle download success: copy file to all required locations.
				for _, path := range pathsToCopy {
					targetPath := filepath.Join(se.workspace.DatasitesDir, path)

					// Resolve any path/type conflicts before writing the file.
					skip, prepErr := se.prepareDownloadTarget(targetPath, pathToMeta[path])
					if prepErr != nil {
						resultsChan <- downloadResult{Path: path, Metadata: pathToMeta[path], Error: prepErr}
						continue
					}
					if skip {
						resultsChan <- downloadResult{Path: path, Metadata: pathToMeta[path], Error: nil}
						continue
					}

					if se.isPriorityFile(targetPath) {
						// a priority file was just downloaded, we don't wanna fire an event for THIS write
						se.watcher.IgnoreOnce(targetPath)
					}

					err := copyLocalWithTmp(res.DownloadPath, targetPath, se.workspace.Root)

					if err != nil {
						resultsChan <- downloadResult{Path: path, Metadata: pathToMeta[path], Error: err}
					} else {
						resultsChan <- downloadResult{Path: path, Metadata: pathToMeta[path], Error: nil}
					}
				}
			}
		}
	}()

	return resultsChan, nil
}

func (se *SyncEngine) getDownloadPriority(meta *FileMetadata) int {
	// file size + key length
	priority := int(meta.Size) + len(meta.Path)

	// user's datasite should be downloaded first
	metaPath := meta.Path.String()
	if strings.HasPrefix(metaPath, se.workspace.Owner) {
		priority = 0
	} else if strings.HasSuffix(metaPath, "syft.pub.yaml") {
		priority = 1
	} else if strings.Contains(metaPath, "/rpc/") {
		priority = 2
	}

	return priority
}

// prepareDownloadTarget ensures the download destination can be written safely.
// It handles file-vs-directory conflicts by comparing timestamps - the newer
// version wins, and the older version is preserved as a conflict marker.
func (se *SyncEngine) prepareDownloadTarget(dst string, meta *FileMetadata) (skip bool, err error) {
	parentDir := filepath.Dir(dst)

	// If the parent exists as a file, we have a file-vs-directory conflict.
	if info, statErr := os.Stat(parentDir); statErr == nil {
		if !info.IsDir() {
			// Local file blocks directory creation - compare timestamps.
			localMtime := info.ModTime()
			remoteMtime := meta.LastModified

			if localMtime.After(remoteMtime) {
				// Local file is newer - skip download, keep local
				slog.Warn("download parent path is file and newer than remote; skipping download",
					"path", meta.Path, "localMtime", localMtime, "remoteMtime", remoteMtime)
				return true, nil
			}

			// Remote is newer - move local file to conflict and create directory
			conflictPath, markErr := SetMarker(parentDir, Conflict)
			if markErr != nil {
				return false, fmt.Errorf("prepare download target: parent is file %s: %w", parentDir, markErr)
			}
			slog.Warn("download parent path is file; preserving as conflict and creating directory",
				"path", meta.Path, "conflictPath", conflictPath, "localMtime", localMtime, "remoteMtime", remoteMtime)
			if err := os.MkdirAll(parentDir, 0o755); err != nil {
				return false, fmt.Errorf("prepare download target: create directory %s: %w", parentDir, err)
			}
		}
	} else if os.IsNotExist(statErr) {
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return false, fmt.Errorf("prepare download target: create parent %s: %w", parentDir, err)
		}
	} else {
		return false, fmt.Errorf("prepare download target: stat parent %s: %w", parentDir, statErr)
	}

	// If the destination already exists as a directory, we have a directory-vs-file conflict.
	if info, statErr := os.Stat(dst); statErr == nil && info.IsDir() {
		localMtime := info.ModTime()
		remoteMtime := meta.LastModified

		if localMtime.After(remoteMtime) {
			// Local directory is newer - skip download, keep local directory
			slog.Warn("download target is directory and newer than remote file; skipping download",
				"path", meta.Path, "localMtime", localMtime, "remoteMtime", remoteMtime)
			return true, nil
		}

		// Remote file is newer - move directory to conflict and proceed with download
		conflictPath := dst + string(Conflict)

		// If conflict path already exists, add timestamp
		if _, err := os.Stat(conflictPath); err == nil {
			timestamp := time.Now().Format("20060102150405")
			conflictPath = fmt.Sprintf("%s%s.%s", dst, string(Conflict), timestamp)
		}

		if err := os.Rename(dst, conflictPath); err != nil {
			return false, fmt.Errorf("prepare download target: move directory to conflict %s -> %s: %w", dst, conflictPath, err)
		}
		slog.Warn("download target is directory; preserving as conflict and downloading file",
			"path", meta.Path, "conflictPath", conflictPath, "localMtime", localMtime, "remoteMtime", remoteMtime)
	}

	return false, nil
}

func copyLocalWithTmp(src, dst, workspaceRoot string) error {
	if err := utils.EnsureParent(dst); err != nil {
		return err
	}

	// Write to unique temp file in a dedicated temp area (outside watcher),
	// then atomic rename. Keeps half-written temp files out of datasites.
	tmpDir := filepath.Join(workspaceRoot, ".syft-tmp")
	if err := utils.EnsureDir(tmpDir); err != nil {
		return err
	}
	tmpPattern := filepath.Base(dst) + ".tmp.*"

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Get source file info to preserve permissions and mod time
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	// Ensure temp file is cleaned up on any failure path
	success := false

	destFile, err := os.CreateTemp(tmpDir, tmpPattern)
	if err != nil {
		return err
	}
	tmpDst := destFile.Name()
	defer func() {
		if !success {
			if destFile != nil {
				destFile.Close()
			}
			os.Remove(tmpDst)
		}
	}()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	if err := destFile.Sync(); err != nil {
		return err
	}

	if err := destFile.Close(); err != nil {
		return err
	}
	destFile = nil // avoid double close in deferred cleanup

	// Preserve source file permissions and modification time
	if err := os.Chmod(tmpDst, sourceInfo.Mode()); err != nil {
		return err
	}
	if err := os.Chtimes(tmpDst, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
		return err
	}

	// Atomic rename - file appears complete or not at all
	if err := renameFile(tmpDst, dst); err != nil {
		// On Windows, Rename does not overwrite existing files. Retry after explicit remove.
		if runtimeGOOS == "windows" && errors.Is(err, fs.ErrExist) {
			if rmErr := os.Remove(dst); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				return rmErr
			}

			if err := renameFile(tmpDst, dst); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	success = true
	return nil
}
