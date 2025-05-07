package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyncLocalStateEmpty(t *testing.T) {
	emptyDir := t.TempDir()
	syncLocalState := NewSyncLocalState(emptyDir)

	state, err := syncLocalState.Scan()
	assert.NoError(t, err)
	assert.NotNil(t, state)
	assert.Equal(t, 0, len(state))
}

func TestSyncLocalStateScanError(t *testing.T) {
	rootDir := "/private"
	syncLocalState := NewSyncLocalState(rootDir)

	state, err := syncLocalState.Scan()
	assert.Error(t, err)
	assert.Nil(t, state)
}
