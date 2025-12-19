package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T, cfgPath, email, dataDir string, refreshToken string) {
	t.Helper()
	cfg := &config.Config{
		Email:        email,
		DataDir:      dataDir,
		ServerURL:    config.DefaultServerURL,
		ClientURL:    config.DefaultClientURL,
		RefreshToken: refreshToken,
		Path:         cfgPath,
		AppsEnabled:  true,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, cfg.Save())
}

func TestAppList_Empty(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "SyftBox")
	cfgPath := filepath.Join(tmp, "config.json")
	writeTestConfig(t, cfgPath, "alice@example.com", dataDir, "")

	out, code := runCLI(t, "--config", cfgPath, "app", "list")
	require.Equal(t, 0, code, out)

	plain := strings.TrimSpace(stripANSI(out))
	require.Contains(t, plain, "No apps installed at '"+filepath.Join(dataDir, "apps")+"'")
}

func TestAppInstallListUninstall_LocalPath(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "SyftBox")
	cfgPath := filepath.Join(tmp, "config.json")
	writeTestConfig(t, cfgPath, "alice@example.com", dataDir, "")

	// Local app is a directory containing run.sh.
	localAppDir := filepath.Join(tmp, "demo-app")
	require.NoError(t, os.MkdirAll(localAppDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(localAppDir, "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0o644))

	out, code := runCLI(t, "--config", cfgPath, "app", "install", localAppDir)
	require.Equal(t, 0, code, out)

	plain := stripANSI(out)
	require.Contains(t, plain, "Installed 'demo-app' at '")

	out, code = runCLI(t, "--config", cfgPath, "app", "list")
	require.Equal(t, 0, code, out)

	plain = stripANSI(out)
	require.Contains(t, plain, "ID      ")
	require.Contains(t, plain, "local.demo-app")
	require.Contains(t, plain, "Source  "+localAppDir+" (local)")

	out, code = runCLI(t, "--config", cfgPath, "app", "uninstall", "local.demo-app")
	require.Equal(t, 0, code, out)
	require.Contains(t, stripANSI(out), "Uninstalled 'local.demo-app'")

	out, code = runCLI(t, "--config", cfgPath, "app", "list")
	require.Equal(t, 0, code, out)
	require.Contains(t, stripANSI(out), "No apps installed at '"+filepath.Join(dataDir, "apps")+"'")
}

