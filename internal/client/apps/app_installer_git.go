package apps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

var ErrGitNotAvailable = errors.New("git is not available on this system")

func installFromGit(ctx context.Context, repo, appDir, branch, tag, commit string) error {
	if !systemGitAvailable() {
		return ErrGitNotAvailable
	}

	// clone the repo
	args := []string{"clone", repo, appDir}

	// if branch is specified, use it
	if branch != "" {
		args = append(args, "--branch", branch)
	} else if tag != "" {
		args = append(args, "--branch", tag)
	}

	// can't shallow clone if commit is specified
	if commit == "" {
		args = append(args, "--depth=1")
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %q: %w", stderr.String(), err)
	}

	// If commit is specified, checkout that commit
	if commit != "" {
		cmd = exec.CommandContext(ctx, "git", "-C", appDir, "checkout", commit)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if err := os.RemoveAll(appDir); err != nil {
				slog.Warn("failed to remove app", "error", err)
			}
			return fmt.Errorf("git checkout failed: %q: %w", stderr.String(), err)
		}
	}

	return nil
}

// systemGitAvailable checks if the "git" executable can be found in the system's PATH.
func systemGitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
