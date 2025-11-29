package sync

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
	gitignore "github.com/sabhiram/go-gitignore"
)

var defaultIgnoreLines = []string{
	// syft
	"syftignore",
	"**/*syftrejected*", // legacy marker
	"**/*syftconflict*", // legacy marker
	"**/*.conflict.*",
	"**/*.rejected.*",
	"*.syft.tmp.*", // temporary files
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
		customRules, err := readIgnoreFile(ignorePath)
		if err != nil {
			slog.Warn("failed to read syftignore file", "path", ignorePath, "error", err)
		} else if len(customRules) > 0 {
			ignoreLines = append(ignoreLines, customRules...)
			slog.Info("loaded syftignore file", "path", ignorePath, "rules", len(customRules))
		}
	}

	s.ignore = gitignore.CompileIgnoreLines(ignoreLines...)
}

func (s *SyncIgnoreList) ShouldIgnore(path string) bool {
	// Convert absolute path to relative path before matching gitignore patterns
	relPath, err := filepath.Rel(s.baseDir, path)
	if err != nil {
		// If we can't get relative path, the file is outside baseDir - don't ignore
		return false
	}
	return s.ignore.MatchesPath(relPath)
}

func readIgnoreFile(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("ignore file path is empty")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open ignore file: %w", err)
	}
	defer file.Close()

	ignoreLines := []string{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// comments, empty lines, and null bytes
		if strings.HasPrefix(line, "#") || line == "" || strings.Contains(line, "\x00") {
			continue
		}

		ignoreLines = append(ignoreLines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading ignore file: %w", err)
	}

	return ignoreLines, nil
}
