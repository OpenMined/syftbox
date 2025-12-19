package sync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func fm(path, etag string) *FileMetadata {
	return &FileMetadata{
		Path:         SyncPath(path),
		ETag:         etag,
		Size:         int64(len(etag)),
		LastModified: time.Unix(0, 0),
	}
}

func TestSyncEngine_Reconcile_TableDriven(t *testing.T) {
	baseDir := t.TempDir()
	ignore := NewSyncIgnoreList(baseDir)
	ignore.Load()

	se := &SyncEngine{
		ignoreList: ignore,
		syncStatus: NewSyncStatus(),
	}

	cases := []struct {
		name          string
		local, remote map[SyncPath]*FileMetadata
		journal       map[SyncPath]*FileMetadata
		expect        func(*ReconcileOperations)
	}{
		{
			name:   "local created uploads remote",
			local:  map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "l1")},
			remote: map[SyncPath]*FileMetadata{},
			journal: map[SyncPath]*FileMetadata{},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.RemoteWrites, 1)
				assert.Equal(t, OpWriteRemote, r.RemoteWrites[SyncPath("alice/public/a.txt")].Type)
			},
		},
		{
			name:   "remote created downloads local",
			local:  map[SyncPath]*FileMetadata{},
			remote: map[SyncPath]*FileMetadata{SyncPath("bob/public/a.txt"): fm("bob/public/a.txt", "r1")},
			journal: map[SyncPath]*FileMetadata{},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.LocalWrites, 1)
				assert.Equal(t, OpWriteLocal, r.LocalWrites[SyncPath("bob/public/a.txt")].Type)
			},
		},
		{
			name:   "local modified uploads",
			local:  map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "l2")},
			remote: map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "l1")},
			journal: map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "l1")},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.RemoteWrites, 1)
				assert.Equal(t, OpWriteRemote, r.RemoteWrites[SyncPath("alice/public/a.txt")].Type)
			},
		},
		{
			name:   "remote modified downloads",
			local:  map[SyncPath]*FileMetadata{SyncPath("bob/public/a.txt"): fm("bob/public/a.txt", "r1")},
			remote: map[SyncPath]*FileMetadata{SyncPath("bob/public/a.txt"): fm("bob/public/a.txt", "r2")},
			journal: map[SyncPath]*FileMetadata{SyncPath("bob/public/a.txt"): fm("bob/public/a.txt", "r1")},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.LocalWrites, 1)
				assert.Equal(t, OpWriteLocal, r.LocalWrites[SyncPath("bob/public/a.txt")].Type)
			},
		},
		{
			name:   "local deleted deletes remote",
			local:  map[SyncPath]*FileMetadata{},
			remote: map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "r1")},
			journal: map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "r1")},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.RemoteDeletes, 1)
				assert.Equal(t, OpDeleteRemote, r.RemoteDeletes[SyncPath("alice/public/a.txt")].Type)
			},
		},
		{
			name:   "remote deleted deletes local",
			local:  map[SyncPath]*FileMetadata{SyncPath("bob/public/a.txt"): fm("bob/public/a.txt", "l1")},
			remote: map[SyncPath]*FileMetadata{},
			journal: map[SyncPath]*FileMetadata{SyncPath("bob/public/a.txt"): fm("bob/public/a.txt", "l1")},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.LocalDeletes, 1)
				assert.Equal(t, OpDeleteLocal, r.LocalDeletes[SyncPath("bob/public/a.txt")].Type)
			},
		},
		{
			name:   "conflict on simultaneous modify",
			local:  map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "l2")},
			remote: map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "r2")},
			journal: map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "l1")},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.Conflicts, 1)
				assert.Equal(t, OpConflict, r.Conflicts[SyncPath("alice/public/a.txt")].Type)
			},
		},
		{
			name:   "cleanup when all deleted relative to journal",
			local:  map[SyncPath]*FileMetadata{},
			remote: map[SyncPath]*FileMetadata{},
			journal: map[SyncPath]*FileMetadata{SyncPath("alice/public/a.txt"): fm("alice/public/a.txt", "l1")},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.Cleanups, 1)
				_, ok := r.Cleanups[SyncPath("alice/public/a.txt")]
				assert.True(t, ok)
			},
		},
		{
			name:   "ignored when empty local file",
			local:  map[SyncPath]*FileMetadata{SyncPath("alice/public/empty.txt"): {Path: SyncPath("alice/public/empty.txt"), ETag: "x", Size: 0}},
			remote: map[SyncPath]*FileMetadata{},
			journal: map[SyncPath]*FileMetadata{},
			expect: func(r *ReconcileOperations) {
				assert.Len(t, r.Ignored, 1)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := se.reconcile(tc.local, tc.remote, tc.journal)
			tc.expect(res)
		})
	}
}
