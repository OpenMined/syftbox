package sync

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/yashgorana/syftbox-go/internal/message"
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

			if strings.HasSuffix(path, ".request") || strings.HasSuffix(path, ".response") || strings.HasSuffix(path, ".txt") || strings.HasSuffix(path, "rpc.schema.json") {
				sm.writePriority(path)
			} else {
				slog.Debug("fs event ignored ", "event", "WRITE", "path", path)
				// sm.handleEvent(ctx, event.Path)
			}

		case event, ok := <-sm.pollEvents:
			if !ok {
				return
			}
			slog.Debug("fs event ignored", "event", event.Op, "path", event.Path)
			// sm.handleEvent(ctx, event.Path)
		}
	}
}

func (sm *SyncManager) writePriority(path string) {
	timeNow := time.Now()
	// 1. as datasite path
	dsPath := sm.datasite.ToDatasitePath(path)

	// 2. get file info
	fileInfo, err := getFileInfo(path)
	if err != nil {
		slog.Error("priority write stat error", "error", err, "path", path)
		return
	}

	timeTaken := timeNow.Sub(fileInfo.ModTime)
	slog.Info("priority write", "path", dsPath, "size", fileInfo.Size, "etag", fileInfo.Etag, "watchLatency", timeTaken)

	// 3. send rpc message
	message := message.NewFileWrite(
		dsPath.Relative,
		fileInfo.Etag,
		fileInfo.Size,
		fileInfo.Content,
	)

	if err := sm.api.SendMessage(message); err != nil {
		slog.Error("priority write error", "error", err)
	}
}
