//go:build !windows

package appsv2

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

	command.WriteString("echo \"[syftbox] Script PID: $$\"; ")
	command.WriteString("echo \"[syftbox] Start time: $(date)\"; ")
	command.WriteString("echo \"[syftbox] App ID: $SYFTBOX_APP_ID\"; ")
	command.WriteString("echo \"[syftbox] App Dir: $SYFTBOX_APP_DIR\"; ")
	command.WriteString("echo \"[syftbox] App Port: $SYFTBOX_APP_PORT\"; ")
	command.WriteString("echo \"[syftbox] Client Config Path: $SYFTBOX_CLIENT_CONFIG_PATH\"; ")
	command.WriteString(fmt.Sprintf("exec \"%s\"; ", scriptPath))

	return shell, []string{"-lc", command.String()}
}
