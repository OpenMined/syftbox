package sync

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/rjeczalik/notify"
	"github.com/yashgorana/syftbox-go/internal/client/workspace"
	"github.com/yashgorana/syftbox-go/internal/syftsdk"
)

type SyncManager struct {
	sdk      *syftsdk.SyftSDK
	datasite *workspace.Workspace

	watchedEvents chan notify.EventInfo

	syncd map[string]bool
	mu    sync.Mutex
	wg    sync.WaitGroup
}

func NewManager(sdk *syftsdk.SyftSDK, datasite *workspace.Workspace) *SyncManager {
	return &SyncManager{
		sdk:           sdk,
		datasite:      datasite,
		watchedEvents: make(chan notify.EventInfo, 16),
		syncd:         make(map[string]bool),
	}
}

func (sm *SyncManager) Start(ctx context.Context) error {
	slog.Info("sync start")
	slog.Warn("syncing only RPC messages")

	if err := sm.startWatcher(ctx); err != nil {
		return err
	}

	if err := sm.sdk.Events.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect websocket: %w", err)
	}

	sm.wg.Add(1)
	go func() {
		defer sm.wg.Done()
		sm.handleSocketEvents(ctx)
	}()

	sm.wg.Add(1)
	go func() {
		defer sm.wg.Done()
		sm.handleFileEvents(ctx)
	}()

	return nil
}

func (sm *SyncManager) Stop() {
	sm.wg.Wait()
	close(sm.watchedEvents)
	slog.Info("sync stop")
}

// ---- file watcher ----

func (sm *SyncManager) startWatcher(ctx context.Context) error {
	if err := sm.startFileWatcher(ctx); err != nil {
		return fmt.Errorf("fs notify error: %w", err)
	}

	return nil
}

func (sm *SyncManager) startFileWatcher(ctx context.Context) error {
	recursivePath := sm.datasite.DatasitesDir + "/..."
	chanEvents := make(chan notify.EventInfo, 16)

	slog.Info("fs notify start", "dir", sm.datasite.DatasitesDir)
	if err := notify.Watch(recursivePath, chanEvents, notify.Write); err != nil {
		return fmt.Errorf("fs notify error: %w", err)
	}

	// Event deduplication map with mutex
	var (
		mu              sync.RWMutex
		lastEvent       = make(map[string]string)
		cleanupInterval = 100 * time.Millisecond
	)

	sm.wg.Add(2)

	// instead of stalling the main event loop
	// lets events in the last 50ms and ignore in the main loop
	// this is a simple way to avoid duplicate events
	go func() {
		defer sm.wg.Done()
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				mu.Lock()
				clear(lastEvent)
				mu.Unlock()

			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer slog.Info("fs notify stop")
		defer sm.wg.Done()

		for {
			select {
			case event, ok := <-chanEvents:
				if !ok {
					return
				}

				mu.RLock()
				_, ok = lastEvent[event.Path()]
				mu.RUnlock()
				if ok {
					continue
				}

				mu.Lock()
				lastEvent[event.Path()] = event.Path()
				mu.Unlock()

				sm.watchedEvents <- event

			case <-ctx.Done():
				notify.Stop(chanEvents)
				return
			}
		}
	}()

	return nil
}

// ----

type FileInfo struct {
	Size    int64
	Etag    string
	ModTime time.Time
	Content []byte
}

func getFileInfo(path string) (*FileInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	isDir := stat.IsDir()
	size := stat.Size()
	modTime := stat.ModTime()

	if isDir {
		return nil, fmt.Errorf("path is a directory")
	} else if size > 1048576 {
		return nil, fmt.Errorf("file size limit exceeded")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create a buffer to store the file content
	var buffer bytes.Buffer
	// Create an MD5 hasher
	hasher := md5.New()

	// Use MultiWriter to write to both the buffer and the hasher at once
	multiWriter := io.MultiWriter(&buffer, hasher)

	if _, err := io.Copy(multiWriter, file); err != nil {
		return nil, err
	}

	return &FileInfo{
		Size:    size,
		Etag:    fmt.Sprintf("%x", hasher.Sum(nil)),
		ModTime: modTime,
		Content: buffer.Bytes(),
	}, nil
}
