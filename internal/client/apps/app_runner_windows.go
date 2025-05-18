//go:build windows

package apps

import (
	"context"
	"os/exec"
)

func (a *App) buildCommand(ctx context.Context, runScript string) *exec.Cmd {
	// the expectation is that they have git-bash installed. if not it will just error out.
	shell := "C:\\Program Files\\Git\\bin\\bash.exe"
	return exec.CommandContext(ctx, shell, "-l", runScript)
}
