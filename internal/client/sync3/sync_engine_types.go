package sync3

type BatchLocalDelete map[string]*SyncOperation
type BatchRemoteDelete map[string]*SyncOperation
type BatchLocalWrite map[string]*SyncOperation
type BatchRemoteWrite map[string]*SyncOperation
type BatchConflict map[string]*SyncOperation

type ReconcileResult struct {
	LocalWrites    BatchLocalWrite
	RemoteWrites   BatchRemoteWrite
	LocalDeletes   BatchLocalDelete
	RemoteDeletes  BatchRemoteDelete
	Conflicts      BatchConflict
	UnchangedPaths []string
	Cleanups       []string
}
