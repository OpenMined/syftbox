package sync

// BatchLocalDelete represents a collection of sync operations for items deleted locally.
type BatchLocalDelete map[string]*SyncOperation

// BatchRemoteDelete represents a collection of sync operations for items deleted remotely.
type BatchRemoteDelete map[string]*SyncOperation

// BatchLocalWrite represents a collection of sync operations for items created or updated locally.
type BatchLocalWrite map[string]*SyncOperation

// BatchRemoteWrite represents a collection of sync operations for items created or updated remotely.
type BatchRemoteWrite map[string]*SyncOperation

// BatchConflict represents a collection of sync operations where conflicts were detected.
type BatchConflict map[string]*SyncOperation

// BatchUnchanged represents a set of paths that were compared and found to be unchanged.
type BatchUnchanged map[string]struct{}

// BatchCleanups represents a set of paths that require local cleanup (e.g., removing tombstones).
type BatchCleanups map[string]struct{}

// BatchIgnored represents a set of paths that were ignored.
type BatchIgnored map[string]struct{}

// ReconcileOperations aggregates the results of a sync reconciliation process,
// categorizing operations based on their type and origin.
type ReconcileOperations struct {
	LocalWrites    BatchLocalWrite
	RemoteWrites   BatchRemoteWrite
	LocalDeletes   BatchLocalDelete
	RemoteDeletes  BatchRemoteDelete
	Conflicts      BatchConflict
	UnchangedPaths BatchUnchanged
	Cleanups       BatchCleanups
	Ignored        BatchIgnored
}

// NewReconcileOperations initializes and returns an empty ReconcileOperations struct.
func NewReconcileOperations() *ReconcileOperations {
	return &ReconcileOperations{
		LocalWrites:    make(BatchLocalWrite),
		RemoteWrites:   make(BatchRemoteWrite),
		LocalDeletes:   make(BatchLocalDelete),
		RemoteDeletes:  make(BatchRemoteDelete),
		Conflicts:      make(BatchConflict),
		UnchangedPaths: make(BatchUnchanged),
		Cleanups:       make(BatchCleanups),
		Ignored:        make(BatchIgnored),
	}
}

// HasChanges returns true if there are any pending write, delete, conflict,
// or cleanup operations resulting from the reconciliation.
func (r *ReconcileOperations) HasChanges() bool {
	return len(r.LocalWrites) > 0 ||
		len(r.RemoteWrites) > 0 ||
		len(r.LocalDeletes) > 0 ||
		len(r.RemoteDeletes) > 0 ||
		len(r.Conflicts) > 0 ||
		len(r.Cleanups) > 0
}
