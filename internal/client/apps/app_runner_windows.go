//go:build windows

package apps

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

func (a *App) buildCommand(ctx context.Context, runScript string) *exec.Cmd {
	var command strings.Builder

	// the expectation is that they have git-bash installed. if not it will just error out.
	shell := "C:\\Program Files\\Git\\bin\\bash.exe"

	syftboxDesktopBinariesPath := os.Getenv("SYFTBOX_DESKTOP_BINARIES_PATH")
	if syftboxDesktopBinariesPath != "" {
		command.WriteString("export PATH=$PATH:" + syftboxDesktopBinariesPath + "; ")
	}

	command.WriteString("RUN_SH_PATH=$(cygpath -u '" + runScript + "'); ")
	command.WriteString("chmod +x $RUN_SH_PATH; ")
	command.WriteString("exec $RUN_SH_PATH;")

	return exec.CommandContext(ctx, shell, "-l", "-c", command.String())
}
