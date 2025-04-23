package sync3

import gitignore "github.com/sabhiram/go-gitignore"

var defaultPriorityFiles = []string{
	"**/*.request",
	"**/*.response",
}

type SyncPriority struct {
	baseDir  string
	priority *gitignore.GitIgnore
}

func NewSyncPriority(baseDir string) *SyncPriority {
	priority := gitignore.CompileIgnoreLines(defaultPriorityFiles...)
	return &SyncPriority{baseDir: baseDir, priority: priority}
}

func (s *SyncPriority) ShouldPrioritize(path string) bool {
	return s.priority.MatchesPath(path)
}
