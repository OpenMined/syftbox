package sync

import (
	"log/slog"
	"os"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/client/subscriptions"
)

func (se *SyncEngine) subscriptionAction(path string) subscriptions.Action {
	if se.subs == nil {
		return subscriptions.ActionBlock
	}
	cfg := se.subs.Get()
	return cfg.ActionForPath(se.workspace.Owner, path)
}

func (se *SyncEngine) shouldSyncPath(path string) bool {
	if aclspec.IsACLFile(path) {
		return true
	}
	if subscriptions.IsSubFile(path) {
		return false
	}
	action := se.subscriptionAction(path)
	return action == subscriptions.ActionAllow
}

func (se *SyncEngine) shouldPrunePath(path string) bool {
	if aclspec.IsACLFile(path) || subscriptions.IsSubFile(path) {
		return false
	}
	action := se.subscriptionAction(path)
	return action == subscriptions.ActionBlock
}

func (se *SyncEngine) pruneBlockedPaths(localState, journalState map[SyncPath]*FileMetadata) {
	for path := range localState {
		rel := path.String()
		if !se.shouldPrunePath(rel) {
			continue
		}

		abs := se.workspace.DatasiteAbsPath(rel)
		if err := os.RemoveAll(abs); err != nil && !os.IsNotExist(err) {
			slog.Warn("subscriptions prune failed", "path", rel, "error", err)
			continue
		}

		delete(localState, path)
		delete(journalState, path)
		se.journal.Delete(path)
		se.syncStatus.SetCompletedAndRemove(path)

	}
}
