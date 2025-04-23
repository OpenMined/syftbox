package sync3

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
		events:   make(chan notify.EventInfo),
	}
}

func (fw *FileWatcher) Start(ctx context.Context) error {
	slog.Info("file watcher start", "dir", fw.watchDir)

	recursivePath := fw.watchDir + "/..."
	if err := notify.Watch(recursivePath, fw.events, notify.Write); err != nil {
		return err
	}
	return nil
}

func (fw *FileWatcher) Stop() {
	notify.Stop(fw.events)
	close(fw.events)
	slog.Info("file watcher stop")
}

func (fw *FileWatcher) Events() <-chan notify.EventInfo {
	return fw.events
}
