package sync

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/openmined/syftbox/internal/utils"
	gitignore "github.com/sabhiram/go-gitignore"
)

var defaultIgnoreLines = []string{
	// syft
	"syftignore",
	"**/*syftrejected*",
	"**/*syftconflict*",
	".syftkeep",
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
	".git",
	"*.tmp",
	"*.log",
	"logs/",
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
	return &SyncIgnoreList{baseDir: baseDir}
}

func (s *SyncIgnoreList) Load() {
	ignorePath := filepath.Join(s.baseDir, "syftignore")
	ignoreLines := defaultIgnoreLines

	// read the syftignore file if it exists
	if utils.FileExists(ignorePath) {
		rules := 0
		file, err := os.Open(ignorePath)
		if err != nil {
			slog.Warn("Failed to open syftignore file", "path", ignorePath, "error", err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				ignoreLines = append(ignoreLines, line)
				rules++
			}
		}

		// Check for errors during the scan
		if err := scanner.Err(); err != nil {
			slog.Warn("Error reading syftignore file", "path", ignorePath, "error", err)
		} else {
			slog.Info("Loaded syftignore file", "path", ignorePath, "rules", rules)
		}
	}

	s.ignore = gitignore.CompileIgnoreLines(ignoreLines...)
}

func (s *SyncIgnoreList) ShouldIgnore(path string) bool {
	return s.ignore.MatchesPath(path)
}
