//go:build !windows

package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func buildRunnerArgs(scriptPath string) (string, []string) {
	var command strings.Builder
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}

	// shell login doesn't seem to always source the profile
	// so we just load it up
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

	// Log environment info
	logVars := []string{
		"Script PID: $$",
		"Start time: $(date)",
		"App ID: $SYFTBOX_APP_ID",
		"App Dir: $SYFTBOX_APP_DIR",
		"App Port: $SYFTBOX_APP_PORT",
		"Client Config Path: $SYFTBOX_CLIENT_CONFIG_PATH",
		"PATH: $PATH",
	}

	for _, msg := range logVars {
		command.WriteString(fmt.Sprintf("echo \"[syftbox] %s\"; ", msg))
	}
	command.WriteString("echo \"==========STARTING APP==========\"; ")

	command.WriteString(fmt.Sprintf("exec \"%s\"; ", scriptPath))

	return shell, []string{"-lc", command.String()}
}
