package sync

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/yashgorana/syftbox-go/internal/syftmsg"
)

func (sm *SyncManager) handleFileEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-sm.watchedEvents:
			if !ok {
				return
			}

			path := event.Path()

			if sm.shouldIgnorePath(path) {
				continue
			}

			if strings.HasSuffix(path, ".request") || strings.HasSuffix(path, ".response") || strings.HasSuffix(path, "syftperm.yaml") || strings.HasSuffix(path, "rpc.schema.json") {
				sm.writePriority(path)
			} else {
				// sm.handleEvent(ctx, event.Path)
			}
		}
	}
}

func (sm *SyncManager) writePriority(path string) {
	timeNow := time.Now()

	fileInfo, err := getFileInfo(path)
	if err != nil {
		slog.Error("priority write stat error", "error", err, "path", path)
		return
	}

	timeTaken := timeNow.Sub(fileInfo.ModTime)
	relPath := sm.datasite.DatasiteRelPath(path)
	slog.Info("priority write", "path", relPath,
		"size", fileInfo.Size,
		"etag", fileInfo.Etag,
		"watchLatency", timeTaken)

	message := syftmsg.NewFileWrite(
		relPath,
		fileInfo.Etag,
		fileInfo.Size,
		fileInfo.Content,
	)

	if err := sm.sdk.Events.Send(message); err != nil {
		slog.Error("priority write error", "error", err)
	}
}
