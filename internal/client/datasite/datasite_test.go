package datasite

import (
	"path/filepath"
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatasite_New_WiresComponents(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		DataDir:   tmp,
		Email:     "Alice@Example.com",
		ServerURL: "http://127.0.0.1:8080",
		Path:      filepath.Join(tmp, "config.json"),
	}

	ds, err := New(cfg)
	require.NoError(t, err)

	assert.NotNil(t, ds.GetWorkspace())
	assert.NotNil(t, ds.GetSDK())
	assert.NotNil(t, ds.GetSyncManager())
	assert.NotNil(t, ds.GetAppManager())
	assert.NotNil(t, ds.GetAppScheduler())

	// Config should be validated/normalized in New.
	assert.Equal(t, "alice@example.com", ds.GetConfig().Email)
	assert.True(t, filepath.IsAbs(ds.GetConfig().DataDir))
}

func TestDatasite_updateRefreshToken_Idempotence(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		DataDir:      tmp,
		Email:        "alice@example.com",
		ServerURL:    "http://127.0.0.1:8080",
		RefreshToken: "old",
		Path:         filepath.Join(tmp, "config.json"),
	}

	ds, err := New(cfg)
	require.NoError(t, err)

	// Empty or same token should not change.
	ds.updateRefreshToken("")
	assert.Equal(t, "old", ds.config.RefreshToken)
	ds.updateRefreshToken("old")
	assert.Equal(t, "old", ds.config.RefreshToken)

	// New token should update in memory.
	ds.updateRefreshToken("new")
	assert.Equal(t, "new", ds.config.RefreshToken)
}

