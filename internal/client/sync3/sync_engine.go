package sync3

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/syftsdk"
)

const (
	fullSyncInterval = 5 * time.Second
)

var (
	ErrSyncAlreadyRunning = errors.New("sync already running")
)

type SyncEngine struct {
	workspace      *workspace.Workspace
	sdk            *syftsdk.SyftSDK
	journal        *SyncJournal
	syncStatus     *SyncStatus
	watcher        *FileWatcher
	lastLocalState map[string]*FileMetadata
	ignoreList     *SyncIgnoreList
	priorityList   *SyncPriorityList
	wg             sync.WaitGroup
	muSync         sync.Mutex
}

func NewSyncEngine(workspace *workspace.Workspace, sdk *syftsdk.SyftSDK, ignore *SyncIgnoreList, priority *SyncPriorityList, watcher *FileWatcher) (*SyncEngine, error) {
	journalDir := filepath.Join(workspace.InternalDataDir, "sync.db")
	journal, err := NewSyncJournal(journalDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync journal: %w", err)
	}

	return &SyncEngine{
		sdk:            sdk,
		workspace:      workspace,
		journal:        journal,
		watcher:        watcher,
		ignoreList:     ignore,
		priorityList:   priority,
		lastLocalState: make(map[string]*FileMetadata),
		syncStatus:     NewSyncStatus(),
	}, nil
}

func (se *SyncEngine) Start(ctx context.Context) error {
	slog.Info("sync start")

	// run sync once and wait before starting watcher//websocket
	slog.Info("running initial sync")
	se.runFullSync(ctx)

	// connect to websocket
	slog.Info("listening for websocket events")
	if err := se.sdk.Events.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect websocket: %w", err)
	}

	se.wg.Add(1)
	go func() {
		defer se.wg.Done()

		// using a timer and not a ticker to avoid queued ticks when
		// runFullSync takes more than fullSyncInterval to complete
		timer := time.NewTimer(fullSyncInterval)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():

				return
			case <-timer.C:
				err := se.runFullSync(ctx)
				if err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("failed to run sync", "error", err)
					return
				}
				timer.Reset(fullSyncInterval)
			}
		}
	}()

	se.wg.Add(1)
	go func() {
		defer se.wg.Done()
		se.handleSocketEvents(ctx)
	}()

	se.wg.Add(1)
	go func() {
		defer se.wg.Done()
		se.handleWatcherEvents(ctx)
	}()

	return nil
}

func (se *SyncEngine) Stop() error {
	slog.Info("sync stop")
	return se.journal.Close()
}

// RunSync performs a full sync of the local and remote states
func (se *SyncEngine) RunSync(ctx context.Context) error {
	return se.runFullSync(ctx)
}

func (se *SyncEngine) runFullSync(ctx context.Context) error {
	if !se.muSync.TryLock() {
		return ErrSyncAlreadyRunning
	}
	defer se.muSync.Unlock()

	tstart := time.Now()

	remoteState, err := se.getRemoteState(ctx)
	if err != nil {
		return fmt.Errorf("get remote state: %w", err)
	}

	localState, err := se.getLocalState(ctx)
	if err != nil {
		return err
	}

	// journal is empty, but you have local files!
	journalCount, err := se.journal.Count()
	if err != nil {
		return fmt.Errorf("get journal count: %w", err)
	}

	if journalCount == 0 && len(localState) > 0 && len(remoteState) > 0 {
		slog.Info("rebuilding journal")
		se.rebuildJournal(localState, remoteState)
	}

	journalState, err := se.journal.GetState()
	if err != nil {
		return fmt.Errorf("get journal state: %w", err)
	}
	result := se.reconcile(localState, remoteState, journalState)
	se.executeReconcileOperations(ctx, result)

	if len(result.LocalWrites) > 0 || len(result.RemoteWrites) > 0 || len(result.RemoteDeletes) > 0 || len(result.Conflicts) > 0 {
		slog.Info("full sync", "took", time.Since(tstart),
			"downloads", len(result.LocalWrites),
			"uploads", len(result.RemoteWrites),
			"deletes", len(result.RemoteDeletes),
			"conflicts", len(result.Conflicts),
			"unchanged", len(result.UnchangedPaths),
			"cleanups", len(result.Cleanups),
			"syncing", se.syncStatus.Count(),
		)
	}

	return nil
}

func (se *SyncEngine) reconcile(localState, remoteState, journalState map[string]*FileMetadata) *ReconcileOperations {
	allPaths := make(map[string]struct{})
	reconcileOps := NewReconcileOperations()

	for path := range journalState {
		allPaths[path] = struct{}{}
	}
	for path := range localState {
		allPaths[path] = struct{}{}
	}
	for path := range remoteState {
		allPaths[path] = struct{}{}
	}

	ignored := 0

	for path := range allPaths {
		local, localExists := localState[path]
		remote, remoteExists := remoteState[path]
		journal, journalExists := journalState[path]

		// check if it's already in conflict
		if se.isConflict(path) || se.isSyncing(path) || se.ignoreList.ShouldIgnore(path) {
			ignored++
			continue
		}

		localModified := localExists && journalExists && se.hasModified(local, journal)
		remoteModified := remoteExists && journalExists && se.hasModified(remote, journal)
		localCreated := localExists && !journalExists && !remoteExists
		remoteCreated := remoteExists && !journalExists && !localExists
		localDeleted := !localExists && journalExists && remoteExists
		remoteDeleted := !remoteExists && journalExists && localExists

		// early checks
		if !localExists && !remoteExists && journalExists {
			// Both deleted cleanly (relative to journal)
			reconcileOps.Cleanups[path] = struct{}{}
			continue
		}

		// conflicts
		if (localModified && remoteModified) ||
			(localCreated && remoteCreated) {
			// Conflict Case: Local Create/Modify + Remote Create/Modify
			// todo we can also consider local modify + remote delete or local delete + remote modify as conflict
			reconcileOps.Conflicts[path] = &SyncOperation{Op: OpConflict, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
			continue
		}

		// Regular Sync
		if localCreated || localModified {
			// Local New/Modify + Remote Unchanged
			reconcileOps.RemoteWrites[path] = &SyncOperation{Op: OpWriteRemote, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
		} else if remoteCreated || remoteModified {
			// Local Unchanged + Remote New/Modify
			reconcileOps.LocalWrites[path] = &SyncOperation{Op: OpWriteLocal, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
		} else if localDeleted {
			// Local Delete + Remote Exists
			reconcileOps.RemoteDeletes[path] = &SyncOperation{Op: OpDeleteRemote, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
		} else if remoteDeleted {
			// Remote Delete + Local Exists
			reconcileOps.LocalDeletes[path] = &SyncOperation{Op: OpDeleteLocal, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
		} else {
			// Local Unchanged + Remote Unchanged
			reconcileOps.UnchangedPaths[path] = struct{}{}
			continue
		}
	}

	return reconcileOps
}

func (se *SyncEngine) executeReconcileOperations(ctx context.Context, result *ReconcileOperations) {
	var wg sync.WaitGroup
	// run batch operations in parallel
	wg.Add(1)
	go func() {
		defer wg.Done()
		se.handleRemoteWrites(ctx, result.RemoteWrites)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		se.handleLocalWrites(ctx, result.LocalWrites)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		se.handleRemoteDeletes(ctx, result.RemoteDeletes)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		se.handleLocalDeletes(ctx, result.LocalDeletes)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		se.handleConflicts(ctx, result.Conflicts)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		// cleanup the journal
		for path := range result.Cleanups {
			se.journal.Delete(path)
		}
	}()

	wg.Wait()
}

func (se *SyncEngine) hasModified(f1, f2 *FileMetadata) bool {
	// Both missing
	if f1 == nil && f2 == nil {
		return false
	}

	// One exists, one doesn't (Create or Delete relative to the other)
	if f1 == nil || f2 == nil {
		return true
	}

	// Both exist, compare metadata
	// NOTE: Ensure Version/ETag are populated correctly for local vs journal/remote comparison.
	// If comparing local vs journal, local might not have Version/ETag from server yet.

	// Option 1: Prioritize Version if available on both
	// Assumes Version is the server-authoritative version
	if f1.Version != "" && f2.Version != "" { // Use Version field name from previous discussion
		return f1.Version != f2.Version
	}

	// Option 2: Use ETag/Hash if VersionID isn't reliable or available
	// Need clarity on whether f1.ETag represents local hash or server ETag
	if f1.ETag != "" && f2.ETag != "" {
		return f1.ETag != f2.ETag
	}

	// Option 3: Fallback to Size (more reliable than ModTime)
	if f1.Size != f2.Size {
		return true
	}

	// Option 4: Fallback to ModTime (use cautiously, with tolerance?)
	if f1.LastModified != f2.LastModified {
		return true
	}

	// If we reach here, consider them unmodified relative to each other
	return false
}

func (se *SyncEngine) isSyncing(path string) bool {
	return se.syncStatus.IsSyncing(path)
}

func (se *SyncEngine) isConflict(path string) bool {
	// if there's a dir basename.conflicted/
	name := filepath.Base(path)
	conflictedDir := filepath.Join(filepath.Dir(path), name+".conflicted")
	info, err := os.Stat(conflictedDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func (se *SyncEngine) getRemoteState(ctx context.Context) (map[string]*FileMetadata, error) {
	// tstart := time.Now()
	resp, err := se.sdk.Datasite.GetView(ctx, &syftsdk.DatasiteViewParams{})
	if err != nil {
		return nil, err
	}
	// slog.Debug("remote state", "took", time.Since(tstart), "files", len(resp.Files))

	remoteState := make(map[string]*FileMetadata)
	for _, file := range resp.Files {
		remoteState[file.Key] = &FileMetadata{
			Path:         file.Key,
			ETag:         file.ETag,
			Size:         int64(file.Size),
			LastModified: file.LastModified,
			Version:      "",
		}
	}

	// slog.Debug("build remote state", "files", len(remoteState), "took", time.Since(tstart))
	return remoteState, nil
}

func (se *SyncEngine) getLocalState(ctx context.Context) (map[string]*FileMetadata, error) {
	rootDir := se.workspace.DatasitesDir
	// tstart := time.Now()
	newState := make(map[string]*FileMetadata)

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk error: %w", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return err
		}

		lastState, ok := se.lastLocalState[relPath]
		if ok && lastState.Size == info.Size() && lastState.LastModified == info.ModTime() {
			newState[relPath] = lastState
			return nil
		}

		// Calculate ETag
		file, err := os.Open(path)
		if err != nil {
			slog.Error("failed to open file", "error", err)
		}
		defer file.Close()

		h := md5.New()
		if _, err := io.Copy(h, file); err != nil {
			slog.Error("failed to copy file", "error", err)
		}

		metadata := &FileMetadata{
			Path:         relPath,
			ETag:         fmt.Sprintf("%x", h.Sum(nil)),
			Size:         info.Size(),
			LastModified: info.ModTime(),
			Version:      "",
		}
		se.lastLocalState[relPath] = metadata
		newState[relPath] = metadata

		return nil
	}

	if err := filepath.WalkDir(rootDir, walkFn); err != nil {
		return nil, err
	}

	// slog.Debug("build local state", "files", len(newState), "took", time.Since(tstart))
	return newState, nil
}

func (se *SyncEngine) rebuildJournal(localState, remoteState map[string]*FileMetadata) {
	allPaths := make(map[string]struct{})
	for path := range localState {
		allPaths[path] = struct{}{}
	}
	for path := range remoteState {
		allPaths[path] = struct{}{}
	}

	for path := range allPaths {
		local, localExists := localState[path]
		remote, remoteExists := remoteState[path]

		if localExists && remoteExists && local.ETag == remote.ETag {
			se.journal.Set(local)
		}
	}
}

func (se *SyncEngine) handleSocketEvents(ctx context.Context) {
	socketEvents := se.sdk.Events.Get()
	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-socketEvents:
			if !ok {
				slog.Debug("handleSocketEvents channel closed")
				return
			}

			switch msg.Type {
			case syftmsg.MsgSystem:
				se.handleSystem(msg)
			case syftmsg.MsgError:
				se.handlePriorityError(msg)
			case syftmsg.MsgFileWrite:
				se.handlePriorityDownload(msg)
			default:
				slog.Debug("websocket unhandled type", "type", msg.Type)
			}
		}
	}
}

func (se *SyncEngine) handleWatcherEvents(ctx context.Context) {
	watcherEvents := se.watcher.Events()
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-watcherEvents:
			if !ok {
				return
			}

			path := event.Path()

			if se.ignoreList.ShouldIgnore(path) {
				continue
			}

			if se.priorityList.ShouldPrioritize(path) {
				se.handlePriorityUpload(path)
			}
		}
	}
}

func (se *SyncEngine) handleSystem(msg *syftmsg.Message) {
	systemMsg := msg.Data.(syftmsg.System)
	slog.Info("handle", "msgType", msg.Type, "msgId", msg.Id, "serverVersion", systemMsg.SystemVersion)
}
