package client

import (
	"os"
	"path/filepath"
)

type Workspace struct {
	Root         string
	AppsDir      string
	DatasitesDir string
	LogsDir      string
}

func NewWorkspace(rootDir string) *Workspace {
	return &Workspace{
		Root:         rootDir,
		AppsDir:      filepath.Join(rootDir, "apps"),
		DatasitesDir: filepath.Join(rootDir, "datasites"),
		LogsDir:      filepath.Join(rootDir, "logs"),
	}
}

func (w *Workspace) CreateDirs() error {
	dirs := []string{w.Root, w.AppsDir, w.DatasitesDir, w.LogsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
