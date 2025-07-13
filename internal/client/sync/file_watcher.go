package sync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rjeczalik/notify"
)

const (
	DefaultIgnoreTimeout   = time.Second
	defaultCleanupInterval = 15 * time.Second
	eventBufferSize        = 64
	defaultDebounceTimeout = 50 * time.Millisecond
)

// FilterCallback is a function that returns true if the event should be filtered
type FilterCallback func(path string) bool

type FileWatcher struct {
	watchDir        string
	events          chan notify.EventInfo
	rawEvents       chan notify.EventInfo
	ignore          map[string]time.Time
	ignoreMu        sync.RWMutex
	cleanupInterval time.Duration
	done            chan struct{}
	wg              sync.WaitGroup
	// Debouncing fields
	pendingEvents   map[string]notify.EventInfo
	eventTimers     map[string]*time.Timer
	debounceMu      sync.Mutex
	debounceTimeout time.Duration
	// Raw event filtering
	ignoreCallback FilterCallback
	callbackMu     sync.RWMutex
}

func NewFileWatcher(watchDir string) *FileWatcher {
	return &FileWatcher{
		watchDir:        watchDir,
		events:          nil,
		rawEvents:       nil,
		ignore:          make(map[string]time.Time),
		cleanupInterval: defaultCleanupInterval,
		done:            make(chan struct{}),
		pendingEvents:   make(map[string]notify.EventInfo),
		eventTimers:     make(map[string]*time.Timer),
		debounceTimeout: defaultDebounceTimeout,
	}
}

func (fw *FileWatcher) SetCleanupInterval(interval time.Duration) {
	fw.cleanupInterval = interval
}

// SetDebounceTimeout sets the debounce timeout for events
func (fw *FileWatcher) SetDebounceTimeout(timeout time.Duration) {
	fw.debounceTimeout = timeout
}

// FilterPaths sets a callback function to filter out raw events before debouncing
// The callback should return true if the event should be ignored
func (fw *FileWatcher) FilterPaths(callback FilterCallback) {
	fw.callbackMu.Lock()
	defer fw.callbackMu.Unlock()
	fw.ignoreCallback = callback
}

func (fw *FileWatcher) Start(ctx context.Context) error {
	slog.Info("file watcher start", "dir", fw.watchDir)

	fw.rawEvents = make(chan notify.EventInfo, eventBufferSize)
	fw.events = make(chan notify.EventInfo, eventBufferSize)

	recursivePath := fw.watchDir + "/..."
	if err := notify.Watch(recursivePath, fw.rawEvents, notify.Write); err != nil {
		return err
	}

	// Start the filtering goroutine
	fw.wg.Add(1)
	go fw.filterEvents(ctx)

	// Start the cleanup goroutine for expired entries
	fw.wg.Add(1)
	go fw.cleanupExpiredEntries(ctx)

	return nil
}

func (fw *FileWatcher) Stop() {
	slog.Info("file watcher stopping")

	// Signal all goroutines to stop
	close(fw.done)

	// Stop notify watching (this closes the channel automatically)
	if fw.rawEvents != nil {
		notify.Stop(fw.rawEvents)
	}

	// Wait for all goroutines to finish
	fw.wg.Wait()

	slog.Info("file watcher stopped")
}

func (fw *FileWatcher) Events() <-chan notify.EventInfo {
	return fw.events
}

// IgnoreOnce adds a path to be ignored on the next write event with default timeout
func (fw *FileWatcher) IgnoreOnce(path string) {
	fw.ignoreMu.Lock()
	defer fw.ignoreMu.Unlock()
	expiry := time.Now().Add(DefaultIgnoreTimeout)
	fw.ignore[path] = expiry
}

// IgnoreOnceWithTimeout adds a path to be ignored on the next write event with custom timeout
func (fw *FileWatcher) IgnoreOnceWithTimeout(path string, timeout time.Duration) {
	fw.ignoreMu.Lock()
	defer fw.ignoreMu.Unlock()
	expiry := time.Now().Add(timeout)
	fw.ignore[path] = expiry
}

// isPathTemporarilyIgnored checks if a path was requested to be ignored and removes it if found
func (fw *FileWatcher) isPathTemporarilyIgnored(path string) bool {
	fw.ignoreMu.Lock()
	defer fw.ignoreMu.Unlock()

	expiry, exists := fw.ignore[path]
	if !exists {
		return false
	}

	// Check if expired
	if time.Now().After(expiry) {
		delete(fw.ignore, path)
		return false
	}

	// Path is ignored and not expired, remove it and return true
	delete(fw.ignore, path)
	return true
}

// filterEvents filters out ignored paths, debounces events, and forwards the rest
func (fw *FileWatcher) filterEvents(ctx context.Context) {
	defer func() {
		slog.Debug("file watcher filter events done")

		// Cancel all pending timers and flush pending events
		fw.debounceMu.Lock()
		for path, timer := range fw.eventTimers {
			timer.Stop()
			if event, exists := fw.pendingEvents[path]; exists {
				select {
				case fw.events <- event:
					slog.Debug("file watcher flushing pending event on exit", "event", event)
				default:
					slog.Warn("file watcher channel full during exit, dropping event", "path", path)
				}
			}
		}
		fw.debounceMu.Unlock()

		fw.wg.Done()
		close(fw.events)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fw.done:
			return
		case event, ok := <-fw.rawEvents:
			if !ok {
				return
			}

			if fw.ignoreCallback != nil && fw.ignoreCallback(event.Path()) {
				// Event ignored by callback, skip entirely
				continue
			}

			// Debounce remaining events
			// On linux, as we write to file inotify triggers a BURST of WRITE events until the file is completely written
			// The catch is that there will be a 50ms added latency to the event
			fw.debounceEvent(event)
		}
	}
}

// debounceEvent handles debouncing logic for file events
func (fw *FileWatcher) debounceEvent(event notify.EventInfo) {
	path := event.Path()

	fw.debounceMu.Lock()
	defer fw.debounceMu.Unlock()

	// Cancel existing timer for this path if it exists
	if timer, exists := fw.eventTimers[path]; exists {
		timer.Stop()
		delete(fw.eventTimers, path)
	}

	// Store/update the pending event for this path
	fw.pendingEvents[path] = event

	// Create a new timer to flush this event after the debounce timeout
	timer := time.AfterFunc(fw.debounceTimeout, func() {
		fw.flushEvent(path)
	})

	fw.eventTimers[path] = timer
}

// flushEvent sends the pending event for a path and cleans up
func (fw *FileWatcher) flushEvent(path string) {
	fw.debounceMu.Lock()
	event, exists := fw.pendingEvents[path]
	if !exists {
		fw.debounceMu.Unlock()
		return
	}

	// Clean up
	delete(fw.pendingEvents, path)
	delete(fw.eventTimers, path)
	fw.debounceMu.Unlock()

	// Check if path should be ignored now (when actually sending the event)
	if fw.isPathTemporarilyIgnored(path) {
		return
	}

	// Send the event
	select {
	case fw.events <- event:
		slog.Debug("file watcher", "event", event.Event(), "path", path)
	default:
		slog.Warn("file watcher dropped", "reason", "channel full", "path", path)
	}
}

// cleanupExpiredEntries periodically removes expired entries from the ignore list
func (fw *FileWatcher) cleanupExpiredEntries(ctx context.Context) {
	defer func() {
		fw.wg.Done()
	}()

	ticker := time.NewTicker(fw.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fw.done:
			return
		case <-ticker.C:
			fw.ignoreMu.Lock()
			now := time.Now()
			for path, expiry := range fw.ignore {
				if now.After(expiry) {
					delete(fw.ignore, path)
				}
			}
			fw.ignoreMu.Unlock()
		}
	}
}
