package apps

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidApp_RequiresRunScript(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, IsValidApp(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755))
	assert.True(t, IsValidApp(dir))
}

func TestAppIDAndNameHelpers(t *testing.T) {
	u, err := url.Parse("https://github.com/OpenMined/syftbox-app.git")
	require.NoError(t, err)
	u.Path = "/OpenMined/syftbox-app"
	assert.Equal(t, "com.github.openmined.syftbox-app", appIDFromURL(u))
	assert.Equal(t, "syftbox-app", appNameFromURL(u))

	assert.Equal(t, "local.My-App", appIDFromPath("/tmp/My App"))
	assert.Equal(t, "my app", appNameFromPath("/tmp/My App"))
}

func TestAppManager_ListApps_EmptyWhenAppsDirMissing(t *testing.T) {
	tmp := t.TempDir()
	appsDir := filepath.Join(tmp, "apps-does-not-exist")
	dataDir := filepath.Join(tmp, "meta")
	mgr := NewManager(appsDir, dataDir)

	list, err := mgr.ListApps()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestAppManager_InstallFromPath_InvalidApp(t *testing.T) {
	tmp := t.TempDir()
	appsDir := filepath.Join(tmp, "apps")
	dataDir := filepath.Join(tmp, "meta")
	mgr := NewManager(appsDir, dataDir)

	src := filepath.Join(tmp, "src")
	require.NoError(t, os.MkdirAll(src, 0o755))
	// No run.sh => invalid
	_, err := mgr.InstallApp(context.Background(), AppInstallOpts{URI: src})
	assert.ErrorIs(t, err, ErrInvalidApp)
}

func TestAppManager_PrepareInstallLocation_ForceBehavior(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManager(filepath.Join(tmp, "apps"), filepath.Join(tmp, "meta"))

	target := mgr.getAppDir("id1")
	require.NoError(t, os.MkdirAll(target, 0o755))

	// Without force should error.
	err := mgr.prepareInstallLocation(target, false)
	assert.Error(t, err)

	// With force removes existing dir.
	err = mgr.prepareInstallLocation(target, true)
	require.NoError(t, err)
	_, statErr := os.Stat(target)
	assert.Error(t, statErr)
}
