//go:build windows

package apps

import (
	"fmt"
	"strings"
)

func buildRunnerArgs(scriptPath string) (string, []string) {
	var command strings.Builder
	// the expectation is that they have git-bash installed. if not it will just error out.
	shell := "C:\\Program Files\\Git\\bin\\bash.exe"

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

	command.WriteString("exec \"" + scriptPath + "\";")

	return shell, []string{"-lc", command.String()}
}
