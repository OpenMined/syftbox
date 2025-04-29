package sync

type BatchLocalDelete map[string]*SyncOperation
type BatchRemoteDelete map[string]*SyncOperation
type BatchLocalWrite map[string]*SyncOperation
type BatchRemoteWrite map[string]*SyncOperation
type BatchConflict map[string]*SyncOperation
type UnchangedPaths map[string]struct{}
type Cleanups map[string]struct{}

type ReconcileOperations struct {
	LocalWrites    BatchLocalWrite
	RemoteWrites   BatchRemoteWrite
	LocalDeletes   BatchLocalDelete
	RemoteDeletes  BatchRemoteDelete
	Conflicts      BatchConflict
	UnchangedPaths UnchangedPaths
	Cleanups       Cleanups
}

func NewReconcileOperations() *ReconcileOperations {
	return &ReconcileOperations{
		LocalWrites:    make(BatchLocalWrite),
		RemoteWrites:   make(BatchRemoteWrite),
		LocalDeletes:   make(BatchLocalDelete),
		RemoteDeletes:  make(BatchRemoteDelete),
		Conflicts:      make(BatchConflict),
		UnchangedPaths: make(UnchangedPaths),
		Cleanups:       make(Cleanups),
	}
}
