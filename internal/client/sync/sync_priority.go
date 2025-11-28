package sync

import gitignore "github.com/sabhiram/go-gitignore"

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
	return s.priority.MatchesPath(path)
}
