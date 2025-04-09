package apps

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppManagerInstallRepoBranch(t *testing.T) {
	tempDir := t.TempDir()
	installer := NewManager(tempDir)
	app, err := installer.InstallRepo("https://github.com/OpenMined/ring.git", &RepoOpts{
		Branch: "main",
	}, true)
	assert.NoError(t, err)
	assert.FileExists(t, filepath.Join(tempDir, "ring/run.sh"))
	assert.Equal(t, "ring", app.Name)
	assert.Equal(t, filepath.Join(tempDir, "ring"), app.Path)
}

func TestAppManagerInstallRepoCommit(t *testing.T) {
	tempDir := t.TempDir()
	installer := NewManager(tempDir)
	app, err := installer.InstallRepo("https://github.com/OpenMined/ring.git", &RepoOpts{
		Commit: "40927c554aeff0d936aec737db679dafa03c0124",
	}, true)
	assert.NoError(t, err)
	assert.FileExists(t, filepath.Join(tempDir, "ring/run.sh"))
	assert.Equal(t, "ring", app.Name)
	assert.Equal(t, filepath.Join(tempDir, "ring"), app.Path)
}
