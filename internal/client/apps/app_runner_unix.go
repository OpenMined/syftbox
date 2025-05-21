//go:build !windows

package apps

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (a *App) buildCommand(ctx context.Context, runScript string) *exec.Cmd {
	var command strings.Builder

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}

	shellBase := filepath.Base(shell)

	switch shellBase {
	case "zsh":
		command.WriteString("test -f ~/.zshrc && source ~/.zshrc; ")
	case "bash":
		command.WriteString("test -f ~/.bashrc && source ~/.bashrc; ")
	case "fish":
		command.WriteString("test -f ~/.config/fish/config.fish && source ~/.config/fish/config.fish; ")
	default:
		command.WriteString("test -f ~/.profile && source ~/.profile; ")
	}

	syftboxDesktopBinariesPath := os.Getenv("SYFTBOX_DESKTOP_BINARIES_PATH")
	if syftboxDesktopBinariesPath != "" {
		command.WriteString("export PATH=$PATH:" + syftboxDesktopBinariesPath + "; ")
	}

	command.WriteString("chmod +x " + runScript + "; ")
	command.WriteString("exec " + runScript + ";")

	slog.Debug("running app", "shell", shell, "script", runScript, "command", command.String())

	return exec.CommandContext(ctx, shell, "-l", "-c", command.String())
}
