package sync

import (
	"context"
	"log/slog"

	"github.com/rjeczalik/notify"
)

type FileWatcher struct {
	watchDir string
	events   chan notify.EventInfo
}

func NewFileWatcher(watchDir string) *FileWatcher {
	return &FileWatcher{
		watchDir: watchDir,
		events:   nil,
	}
}

func (fw *FileWatcher) Start(ctx context.Context) error {
	slog.Info("file watcher start", "dir", fw.watchDir)

	fw.events = make(chan notify.EventInfo)
	recursivePath := fw.watchDir + "/..."
	if err := notify.Watch(recursivePath, fw.events, notify.Write); err != nil {
		return err
	}
	return nil
}

func (fw *FileWatcher) Stop() {
	if fw.events != nil {
		notify.Stop(fw.events)
		close(fw.events)
	}
	slog.Info("file watcher stop")
}

func (fw *FileWatcher) Events() <-chan notify.EventInfo {
	return fw.events
}
