package sync

import (
	gitignore "github.com/sabhiram/go-gitignore"
)

var defaultIgnoreLines = []string{
	// syft
	"syftignore",
	"**/*syftrejected*",
	"**/*syftconflict*",
	"logs/",
	// python
	".ipynb_checkpoints/",
	"__pycache__/",
	"*.py[cod]",
	"dist/",
	"venv/",
	".venv/",
	// IDE/Editor-specific
	".vscode",
	".idea",
	// General excludes
	"*.tmp",
	"*.log",
	// OS-specific
	".DS_Store",
	"Thumbds.db",
	"Icon",
}

type SyncIgnoreList struct {
	baseDir string
	ignore  *gitignore.GitIgnore
}

func NewSyncIgnoreList(baseDir string) *SyncIgnoreList {
	ignore := gitignore.CompileIgnoreLines(defaultIgnoreLines...)
	return &SyncIgnoreList{baseDir: baseDir, ignore: ignore}
}

func (s *SyncIgnoreList) ShouldIgnore(path string) bool {
	// todo strip baseDir from relPath
	return s.ignore.MatchesPath(path)
}
