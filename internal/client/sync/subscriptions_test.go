package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/client/subscriptions"
	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/stretchr/testify/require"
)

func TestSubscriptions_BlockPrunesLocal(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.NewWorkspace(root, "alice@example.com")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(ws.DatasitesDir, 0o755))

	subPath := filepath.Join(ws.MetadataDir, subscriptions.FileName)
	require.NoError(t, subscriptions.Save(subPath, &subscriptions.Config{
		Version: 1,
		Defaults: subscriptions.Defaults{
			Action: subscriptions.ActionAllow,
		},
		Rules: []subscriptions.Rule{
			{
				Action:   subscriptions.ActionBlock,
				Datasite: "bob@example.com",
				Path:     "**",
			},
		},
	}))

	journal, err := NewSyncJournal(filepath.Join(ws.MetadataDir, "sync.db"))
	require.NoError(t, err)
	require.NoError(t, journal.Open())
	t.Cleanup(func() { _ = journal.Close() })

	fileRel := "bob@example.com/public/a.txt"
	fileAbs := ws.DatasiteAbsPath(fileRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(fileAbs), 0o755))
	require.NoError(t, os.WriteFile(fileAbs, []byte("x"), 0o644))

	meta := &FileMetadata{
		Path:         SyncPath(fileRel),
		ETag:         "etag1",
		LocalETag:    "etag1",
		Size:         1,
		LastModified: time.Now(),
	}
	require.NoError(t, journal.Set(meta))

	se := &SyncEngine{
		workspace:  ws,
		journal:    journal,
		syncStatus: NewSyncStatus(),
		subs:       NewSubscriptionManager(subPath),
	}

	localState := map[SyncPath]*FileMetadata{
		SyncPath(fileRel): meta,
	}
	journalState := map[SyncPath]*FileMetadata{
		SyncPath(fileRel): meta,
	}

	se.pruneBlockedPaths(localState, journalState)

	_, statErr := os.Stat(fileAbs)
	require.Error(t, statErr)
	require.Len(t, localState, 0)
	require.Len(t, journalState, 0)

	loaded, err := journal.Get(SyncPath(fileRel))
	require.NoError(t, err)
	require.Nil(t, loaded)
}
