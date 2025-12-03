package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/shirou/gopsutil/v4/disk"
)

const (
	minFreeSpace     = 5 * 1024 * 1024 * 1024 // 5GB
	fullSyncInterval = 5 * time.Second        // 5 seconds
	maxRetryCount    = 3
	syncDbName       = "sync.db"
)

var (
	ErrSyncAlreadyRunning = errors.New("sync already running")
)

type SyncEngine struct {
	workspace         *workspace.Workspace
	sdk               *syftsdk.SyftSDK
	journal           *SyncJournal
	localState        *SyncLocalState
	syncStatus        *SyncStatus
	watcher           *FileWatcher
	ignoreList        *SyncIgnoreList
	priorityList      *SyncPriorityList
	lastSyncTime      time.Time
	adaptiveScheduler *AdaptiveSyncScheduler
	wg                sync.WaitGroup
	muSync            sync.Mutex
}

func NewSyncEngine(
	workspace *workspace.Workspace,
	sdk *syftsdk.SyftSDK,
	ignore *SyncIgnoreList,
	priority *SyncPriorityList,
) (*SyncEngine, error) {
	journalPath := filepath.Join(workspace.MetadataDir, syncDbName)
	journal, err := NewSyncJournal(journalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync journal: %w", err)
	}

	watcher := NewFileWatcher(workspace.DatasitesDir)

	localState := NewSyncLocalState(workspace.DatasitesDir)
	syncStatus := NewSyncStatus()
	adaptiveScheduler := NewAdaptiveSyncScheduler()

	return &SyncEngine{
		sdk:               sdk,
		workspace:         workspace,
		watcher:           watcher,
		ignoreList:        ignore,
		priorityList:      priority,
		journal:           journal,
		localState:        localState,
		syncStatus:        syncStatus,
		adaptiveScheduler: adaptiveScheduler,
	}, nil
}

func (se *SyncEngine) Start(ctx context.Context) error {
	slog.Info("sync start")

	// don't put this in the constructor
	// there would be crucial migrations that might happen before the sync engine is started
	// and we don't want to load pre-migrations state
	if err := se.journal.Open(); err != nil {
		return fmt.Errorf("sync journal: %w", err)
	}

	// run sync once and wait before starting watcher//websocket
	slog.Info("running initial sync")
	if err := se.runFullSync(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("initial full sync: %w", err)
	}

	// start the watcher
	slog.Info("starting file watcher")
	se.watcher.FilterPaths(func(path string) bool {
		// ignore all files that are ignored, not priority or that are marked
		return se.isIgnoredFile(path) || !se.isPriorityFile(path) || IsMarkedPath(path)
	})
	if err := se.watcher.Start(ctx); err != nil {
		return fmt.Errorf("file watcher: %w", err)
	}

	// connect to websocket
	slog.Info("listening for websocket events")
	if err := se.sdk.Events.Connect(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("websocket events: %w", err)
	}

	se.wg.Add(1)
	go func() {
		defer se.wg.Done()

		// using a timer and not a ticker to avoid queued ticks when
		// runFullSync takes more than fullSyncInterval to complete
		// Adaptive: start with default interval, adjust based on activity
		interval := se.adaptiveScheduler.GetSyncInterval()
		timer := time.NewTimer(interval)
		defer timer.Stop()

		lastLevel := se.adaptiveScheduler.GetActivityLevel()

		for {
			select {
			case <-ctx.Done():

				return
			case <-timer.C:
				err := se.runFullSync(ctx)
				if err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("full sync", "error", err)
				}

				// Adaptive: adjust interval based on current activity level
				newInterval := se.adaptiveScheduler.GetSyncInterval()
				currentLevel := se.adaptiveScheduler.GetActivityLevel()

				// Log activity level changes
				if currentLevel != lastLevel {
					slog.Info("sync adaptive interval", "level", currentLevel.String(), "interval", newInterval)
					lastLevel = currentLevel
				}

				timer.Reset(newInterval)
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
	// Stop the file watcher first to prevent new operations
	se.watcher.Stop()

	// Wait for all sync operations to complete with timeout
	slog.Info("sync stopping")
	done := make(chan struct{})
	go func() {
		se.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		slog.Warn("sync operations did not complete within timeout, proceeding with shutdown")
	}

	slog.Info("sync stopped")

	// Now it's safe to close resources
	se.syncStatus.Close()
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

	if err := se.presyncChecks(); err != nil {
		return err
	}

	tStart := time.Now()

	// get remote state
	remoteState, err := se.getRemoteState(ctx)
	if err != nil {
		return fmt.Errorf("get remote state: %w", err)
	}
	tRemoteState := time.Since(tStart)

	// get local state
	tlocalStart := time.Now()
	localState, err := se.localState.Scan()
	if err != nil {
		return fmt.Errorf("scan local state: %w", err)
	}
	tLocalState := time.Since(tlocalStart)

	// scan for existing conflicted/rejected files and populate sync status
	if se.isFirstSync() {
		se.initStatusFromMarkers(localState)
	} else {
		se.cleanupResolvedMarkers()
	}

	// check journal
	journalCount, err := se.journal.Count()
	if err != nil {
		return fmt.Errorf("get journal count: %w", err)
	}

	// journal is empty, but you have local files! rebuild
	if journalCount == 0 && len(localState) > 0 && len(remoteState) > 0 {
		slog.Info("rebuilding journal")
		se.rebuildJournal(localState, remoteState)
	}

	// get the journal state
	tjournalStart := time.Now()
	journalState, err := se.journal.GetState()
	if err != nil {
		return fmt.Errorf("get journal state: %w", err)
	}
	tJournalState := time.Since(tjournalStart)

	// reconcile trees
	tReconcileStart := time.Now()
	result := se.reconcile(localState, remoteState, journalState)
	tReconcile := time.Since(tReconcileStart)

	if result.HasChanges() {
		slog.Info("full sync start",
			"downloads", len(result.LocalWrites),
			"uploads", len(result.RemoteWrites),
			"localDeletes", len(result.LocalDeletes),
			"remoteDeletes", len(result.RemoteDeletes),
			"conflicts", len(result.Conflicts), // new set of conflicts in this cycle
		)
	}

	se.executeReconcileOperations(ctx, result)
	tTotal := time.Since(tStart)

	if result.HasChanges() {
		slog.Info("full sync completed",
			"total", result.Total,
			"downloads", len(result.LocalWrites),
			"uploads", len(result.RemoteWrites),
			"localDeletes", len(result.LocalDeletes),
			"remoteDeletes", len(result.RemoteDeletes),
			"unchanged", len(result.UnchangedPaths),
			"cleanups", len(result.Cleanups),
			"ignored", len(result.Ignored),
			"status.syncing", se.syncStatus.GetSyncingFileCount(),
			"status.unresolvedConflicts", se.syncStatus.GetConflictedFileCount(),
			"status.unresolvedRejects", se.syncStatus.GetRejectedFileCount(),
			"ts.remoteState", tRemoteState,
			"ts.localState", tLocalState,
			"ts.journalState", tJournalState,
			"ts.reconcile", tReconcile,
			"ts.total", tTotal,
		)
	}

	se.lastSyncTime = time.Now()
	return nil
}

func (se *SyncEngine) isFirstSync() bool {
	return se.lastSyncTime.IsZero()
}

func (se *SyncEngine) initStatusFromMarkers(localState map[SyncPath]*FileMetadata) {
	for relPath := range localState {
		relPathStr := relPath.String()

		// cleanup legacy markers - randomly shoved here
		// todo - remove this in a future release
		if IsLegacyMarkedPath(relPathStr) {
			if err := os.Remove(se.workspace.DatasiteAbsPath(relPathStr)); err != nil {
				slog.Error("failed to remove legacy marked path", "path", relPathStr, "error", err)
			}
		}

		if !IsMarkedPath(relPathStr) {
			continue
		}

		// check if conflicted or rejected file on a path
		// if so, then set sync status on the ORIGINAL path
		if IsConflictPath(relPathStr) {
			unmarked := GetUnmarkedPath(relPathStr)
			slog.Warn("unresolved conflict", "path", relPath)
			se.syncStatus.SetConflicted(SyncPath(unmarked))
		} else if IsRejectedPath(relPathStr) {
			unmarked := GetUnmarkedPath(relPathStr)
			slog.Warn("unresolved reject", "path", relPath)
			se.syncStatus.SetRejected(SyncPath(unmarked))
		}
	}
}

func (se *SyncEngine) cleanupResolvedMarkers() {
	// check if the conflict or reject is still present on the local filesystem
	// if not, then we can remove it from the sync status
	conflicted := se.syncStatus.GetConflictedFiles()
	rejected := se.syncStatus.GetRejectedFiles()

	for syncPath := range conflicted {
		localAbsPath := se.workspace.DatasiteAbsPath(syncPath.String())
		if !ConflictFileExists(localAbsPath) {
			slog.Info("resolved conflict", "path", syncPath)
			se.syncStatus.SetCompletedAndRemove(syncPath)
		}
	}

	for syncPath := range rejected {
		localAbsPath := se.workspace.DatasiteAbsPath(syncPath.String())
		if !RejectedFileExists(localAbsPath) {
			slog.Info("resolved reject", "path", syncPath)
			se.syncStatus.SetCompletedAndRemove(syncPath)
		}
	}
}

func (se *SyncEngine) presyncChecks() error {
	// check if the workspace is writable
	if !utils.IsWritable(se.workspace.DatasitesDir) {
		return fmt.Errorf("'%s' is not writable", se.workspace.DatasitesDir)
	}

	// check if the workspace has enough free space
	// need atleast 5gb free
	usage, err := disk.Usage(se.workspace.DatasitesDir)
	if err != nil {
		slog.Error("preflight checks: failed to get disk usage", "error", err)
	}
	if usage.Free <= minFreeSpace {
		return fmt.Errorf("not enough free space on disk. %s free", humanize.Bytes(usage.Free))
	}

	return nil
}

func (se *SyncEngine) reconcile(localState, remoteState, journalState map[SyncPath]*FileMetadata) *ReconcileOperations {
	allPaths := make(map[SyncPath]struct{})
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

	reconcileOps.Total = len(allPaths)

	for path := range allPaths {
		local, localExists := localState[path]
		remote, remoteExists := remoteState[path]
		journal, journalExists := journalState[path]

		isSyncing := se.isSyncing(path)
		isIgnored := se.ignoreList.ShouldIgnore(path.String()) // conflicts and rejects are ignored in the list
		isEmpty := false
		errorCount := se.syncStatus.GetErrorCount(path)
		if localExists && local.Size == 0 {
			isEmpty = true
		}

		if isSyncing || isIgnored || isEmpty || errorCount >= maxRetryCount {
			reconcileOps.Ignored[path] = struct{}{}
			continue
		}

		localCreated := localExists && !journalExists && !remoteExists
		remoteCreated := !localExists && !journalExists && remoteExists
		localDeleted := !localExists && journalExists && remoteExists
		remoteDeleted := localExists && journalExists && !remoteExists
		localModified := localExists && se.hasModified(local, journal)
		remoteModified := remoteExists && se.hasModified(journal, remote)

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
			reconcileOps.Conflicts[path] = &SyncOperation{Type: OpConflict, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
			continue
		}

		// Regular Sync
		if localCreated || localModified {
			// Local New/Modify + Remote Unchanged
			reconcileOps.RemoteWrites[path] = &SyncOperation{Type: OpWriteRemote, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
		} else if remoteCreated || remoteModified {
			// Local Unchanged + Remote New/Modify
			reconcileOps.LocalWrites[path] = &SyncOperation{Type: OpWriteLocal, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
		} else if localDeleted {
			// Local Delete + Remote Exists
			reconcileOps.RemoteDeletes[path] = &SyncOperation{Type: OpDeleteRemote, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
		} else if remoteDeleted {
			// Remote Delete + Local Exists
			reconcileOps.LocalDeletes[path] = &SyncOperation{Type: OpDeleteLocal, RelPath: path, Local: local, Remote: remote, LastSynced: journal}
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
		if len(result.RemoteWrites) > 0 {
			se.handleRemoteWrites(ctx, result.RemoteWrites)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(result.LocalWrites) > 0 {
			se.handleLocalWrites(ctx, result.LocalWrites)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(result.RemoteDeletes) > 0 {
			se.handleRemoteDeletes(ctx, result.RemoteDeletes)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(result.LocalDeletes) > 0 {
			se.handleLocalDeletes(ctx, result.LocalDeletes)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(result.Conflicts) > 0 {
			se.handleConflicts(ctx, result.Conflicts)
		}
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

func (se *SyncEngine) isSyncing(path SyncPath) bool {
	status, exists := se.syncStatus.GetStatus(path)
	if !exists {
		return false
	}

	// File is syncing
	if status.SyncState == SyncStateSyncing {
		return true
	}

	// RACE CONDITION FIX: Also treat recently completed files (within 5s) as "syncing"
	// to prevent concurrent reconciliations from re-processing them
	if status.SyncState == SyncStateCompleted && !status.CompletedAt.IsZero() {
		if time.Since(status.CompletedAt) < 5*time.Second {
			return true
		}
	}

	return false
}

func (se *SyncEngine) isPriorityFile(path string) bool {
	return se.priorityList.ShouldPrioritize(path)
}

func (se *SyncEngine) isIgnoredFile(path string) bool {
	return se.ignoreList.ShouldIgnore(path)
}

func (se *SyncEngine) getRemoteState(ctx context.Context) (map[SyncPath]*FileMetadata, error) {
	resp, err := se.sdk.Datasite.GetView(ctx, &syftsdk.DatasiteViewParams{})
	if err != nil {
		return nil, err
	}

	remoteState := make(map[SyncPath]*FileMetadata)
	for _, file := range resp.Files {
		syncRelPath := SyncPath(file.Key)
		remoteState[syncRelPath] = &FileMetadata{
			Path:         syncRelPath,
			ETag:         file.ETag,
			Size:         int64(file.Size),
			LastModified: file.LastModified,
			Version:      "",
		}
	}

	return remoteState, nil
}

func (se *SyncEngine) rebuildJournal(localState, remoteState map[SyncPath]*FileMetadata) {
	allPaths := make(map[SyncPath]struct{})
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
			slog.Info("client received websocket message", "msgType", msg.Type, "msgId", msg.Id)

			// Record activity for adaptive sync (except system messages)
			if msg.Type != syftmsg.MsgSystem {
				se.adaptiveScheduler.RecordActivity()
			}

			switch msg.Type {
			case syftmsg.MsgSystem:
				go se.handleSystem(msg)
			case syftmsg.MsgError:
				go se.handlePriorityError(msg)
			case syftmsg.MsgFileWrite:
				go se.handlePriorityDownload(msg)
			case syftmsg.MsgFileNotify:
				go se.handlePriorityDownload(msg)
			case syftmsg.MsgHttp:
				go se.processHttpMessage(msg)
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

			// this is already filtered
			relPath, _ := se.workspace.DatasiteRelPath(path)
			slog.Info("watcher detected priority file", "path", relPath, "event", event.Event())

			// Record activity for adaptive sync
			se.adaptiveScheduler.RecordActivity()

			go se.handlePriorityUpload(path)
		}
	}
}

func (se *SyncEngine) handleSystem(msg *syftmsg.Message) {
	systemMsg := msg.Data.(syftmsg.System)
	slog.Info("handle socket message", "msgType", msg.Type, "msgId", msg.Id, "serverVersion", systemMsg.SystemVersion)
}
