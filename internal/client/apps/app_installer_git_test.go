package apps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallFromGit_Branch(t *testing.T) {
	if !systemGitAvailable() {
		t.Skip("git not available")
	}

	repoDir := initTestRepo(t)

	// Create a feature branch with different content.
	mustRun(t, repoDir, "git", "checkout", "-b", "feature")
	mustWrite(t, filepath.Join(repoDir, "message.txt"), "feature\n")
	mustRun(t, repoDir, "git", "add", ".")
	mustRun(t, repoDir, "git", "commit", "-m", "feature")

	dst := filepath.Join(t.TempDir(), "app")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installFromGit(ctx, repoDir, dst, "feature", "", ""); err != nil {
		t.Fatalf("installFromGit: %v", err)
	}
	assertFileContains(t, filepath.Join(dst, "message.txt"), "feature\n")
}

func TestInstallFromGit_Tag(t *testing.T) {
	if !systemGitAvailable() {
		t.Skip("git not available")
	}

	repoDir := initTestRepo(t)
	mustRun(t, repoDir, "git", "tag", "v1.0.0")

	dst := filepath.Join(t.TempDir(), "app")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installFromGit(ctx, repoDir, dst, "", "v1.0.0", ""); err != nil {
		t.Fatalf("installFromGit: %v", err)
	}
	assertFileContains(t, filepath.Join(dst, "message.txt"), "main\n")
}

func TestInstallFromGit_CommitCheckout(t *testing.T) {
	if !systemGitAvailable() {
		t.Skip("git not available")
	}

	repoDir := initTestRepo(t)
	commit := strings.TrimSpace(mustOutput(t, repoDir, "git", "rev-parse", "HEAD"))

	// Advance main branch so commit checkout is meaningful.
	mustWrite(t, filepath.Join(repoDir, "message.txt"), "main-2\n")
	mustRun(t, repoDir, "git", "add", ".")
	mustRun(t, repoDir, "git", "commit", "-m", "main-2")

	dst := filepath.Join(t.TempDir(), "app")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installFromGit(ctx, repoDir, dst, "", "", commit); err != nil {
		t.Fatalf("installFromGit: %v", err)
	}
	assertFileContains(t, filepath.Join(dst, "message.txt"), "main\n")
}

func initTestRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	mustRun(t, repoDir, "git", "init")
	mustRun(t, repoDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, repoDir, "git", "config", "user.name", "Test")
	mustRun(t, repoDir, "git", "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(repoDir, "message.txt"), "main\n")
	mustRun(t, repoDir, "git", "add", ".")
	mustRun(t, repoDir, "git", "commit", "-m", "init")
	return repoDir
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v (dir=%s): %v\n%s", name, args, dir, err, string(out))
	}
}

func mustOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v (dir=%s): %v\n%s", name, args, dir, err, string(out))
	}
	return string(out)
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("unexpected file contents in %s: got %q want %q", path, string(got), want)
	}
}
