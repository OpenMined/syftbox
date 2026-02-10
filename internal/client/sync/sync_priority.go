package sync

import (
	"path/filepath"
	"strings"

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
	if relPath == "." || strings.HasPrefix(relPath, "..") {
		relPath = normalizeRelPath(s.baseDir, path)
		if relPath == "" {
			return false
		}
	}
	return s.priority.MatchesPath(relPath)
}

func normalizeRelPath(baseDir, target string) string {
	base := resolvePath(baseDir)
	path := resolvePath(target)
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return ""
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}

func resolvePath(path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		if resolved, err := filepath.Abs(abs); err == nil {
			abs = resolved
		}
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
