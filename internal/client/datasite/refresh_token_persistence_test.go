package datasite

import (
	"path/filepath"
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/stretchr/testify/require"
)

func TestDatasite_updateRefreshToken_PersistsToDisk(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := &config.Config{
		DataDir:      tmp,
		Email:        "alice@example.com",
		ServerURL:    "http://127.0.0.1:8080",
		RefreshToken: "old",
		Path:         cfgPath,
	}

	ds, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, ds.config.Save())

	ds.updateRefreshToken("new")

	loaded, err := config.LoadFromFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, "new", loaded.RefreshToken)
}

