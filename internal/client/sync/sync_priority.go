package sync

import (
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

var defaultPriorityFiles = []string{
	"**/*.request",
	"**/*.response",
	"**/syft.pub.yaml", // ACL files need priority to avoid race condition
}

type SyncPriorityList struct {
	baseDir  string
	priority *gitignore.GitIgnore
}

func NewSyncPriorityList(baseDir string) *SyncPriorityList {
	priority := gitignore.CompileIgnoreLines(defaultPriorityFiles...)
	return &SyncPriorityList{baseDir: baseDir, priority: priority}
}

func (s *SyncPriorityList) ShouldPrioritize(path string) bool {
	// Convert absolute path to relative path before matching gitignore patterns
	relPath, err := filepath.Rel(s.baseDir, path)
	if err != nil {
		// If we can't get relative path, the file is outside baseDir - don't prioritize
		return false
	}
	return s.priority.MatchesPath(relPath)
}
