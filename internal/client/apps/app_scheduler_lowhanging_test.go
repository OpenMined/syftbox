package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppScheduler_EmptyAndErrors(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManager(tmp, tmp)
	s := NewAppScheduler(mgr, "")

	assert.Empty(t, s.GetApps())

	_, err := s.StartApp("missing")
	assert.ErrorIs(t, err, ErrAppNotFound)

	_, err = s.StopApp("missing")
	assert.ErrorIs(t, err, ErrAppNotFound)
}

func TestAppScheduler_RefreshInProgress(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManager(tmp, tmp)
	s := NewAppScheduler(mgr, "")

	// Simulate scan already in progress.
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	err := s.Refresh()
	assert.ErrorIs(t, err, ErrRefreshInProgress)
}

