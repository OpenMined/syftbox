package fswatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/yashgorana/syftbox-go/pkg/utils"
)

var (
	ErrWatcherClosed = errors.New("watcher closed")
	ErrDirNotExist   = errors.New("directory to watch does not exist")
)

type Watcher struct {
	Events chan fsnotify.Event
	Errors chan error

	watcher  *fsnotify.Watcher
	isClosed bool
	mu       sync.Mutex
}

func New() (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		watcher:  watcher,
		Events:   make(chan fsnotify.Event, 16),
		Errors:   make(chan error, 16),
		isClosed: false,
	}, nil
}

func (w *Watcher) Start(ctx context.Context) error {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return ErrWatcherClosed
			}
			w.handleEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return ErrWatcherClosed
			}
			w.handleError(err)

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (w *Watcher) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isClosed {
		return ErrWatcherClosed
	}
	w.isClosed = true
	close(w.Events)
	close(w.Errors)
	return w.watcher.Close()
}

func (w *Watcher) Add(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isClosed {
		return ErrWatcherClosed
	}

	if !utils.DirExists(dir) {
		return ErrDirNotExist
	}

	return w.recursivelyAddWatch(dir)
}

func (w *Watcher) Remove(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isClosed {
		return ErrWatcherClosed
	}

	return w.recursivelyRemoveWatch(dir)
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	if event.Has(fsnotify.Chmod) {
		return
	} else if event.Has(fsnotify.Create) {
		if err := w.onCreate(event); err != nil {
			slog.Error("failed to handle create event", "error", err, "path", event.Name)
			w.handleError(err)
		}
	} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		if err := w.onRemove(event); err != nil {
			slog.Error("failed to handle remove event", "error", err, "path", event.Name)
			w.handleError(err)
		}
	}

	// send event to channel
	select {
	case w.Events <- event:
	default:
		slog.Warn("dropped event: events channel full",
			"path", event.Name,
			"op", event.Op.String(),
		)
	}
}

func (w *Watcher) handleError(err error) {
	// send error to channel
	select {
	case w.Errors <- err:
	default:
		slog.Warn("dropped error: errors channel full",
			"error", err,
		)
	}
}

func (w *Watcher) onCreate(event fsnotify.Event) error {
	fileinfo, err := os.Stat(event.Name)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	if fileinfo.IsDir() {
		if err = w.recursivelyAddWatch(event.Name); err != nil {
			return fmt.Errorf("recursive add watch: %w", err)
		}
	}

	return nil
}

func (w *Watcher) onRemove(event fsnotify.Event) error {
	// can't stat a delete dir/file, so yolo it
	if err := w.watcher.Remove(event.Name); err != nil {
		if !errors.Is(err, fsnotify.ErrNonExistentWatch) {
			slog.Debug("non existent watch", "path", event.Name, "error", err)
			return fmt.Errorf("remove watch: %w", err)
		}
	}
	return nil
}

func (w *Watcher) recursivelyAddWatch(dir string) error {
	slog.Debug("watcher add", "dir", dir)
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk dir: %w", err)
		}
		if d.IsDir() {
			err := w.watcher.Add(path)
			if err != nil {
				return fmt.Errorf("fsnotify add watch: %w", err)
			}
		}
		return nil
	})
}

func (w *Watcher) recursivelyRemoveWatch(dir string) error {
	slog.Debug("watcher remove", "dir", dir)
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk dir: %w", err)
		}
		if d.IsDir() {
			err := w.watcher.Remove(path)
			if err != nil {
				return fmt.Errorf("fsnotify remove watch: %w", err)
			}
		}
		return nil
	})
}
