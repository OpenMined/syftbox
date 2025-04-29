package sync3

import (
	gitignore "github.com/sabhiram/go-gitignore"
)

const defaultIgnoreFile = `
# Syft
syftignore
apps/
private/

# Python
.ipynb_checkpoints/
__pycache__/
*.py[cod]
*$py.class
.venv/
venv/
dist/

# OS-specific
.DS_Store
Thumbds.db
Icon

# IDE/Editor-specific
*.swp
*.swo
.vscode/
.idea/
*.iml

# General excludes
*.tmp
*.log

# excluded datasites
# example:
# /user_to_exclude@example.com/
`

var defaultIgnoreLines = []string{
	"syftignore",
	"**/*syftrejected*",
	"**/*syftconflict*",
	"logs/",
	".ipynb_checkpoints/",
	"__pycache__/",
	"*.py[cod]",
	"dist/",
	"venv/",
	".venv/",
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
