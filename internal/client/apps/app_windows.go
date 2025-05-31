//go:build windows

package appsv2

import (
	"strings"
)

func buildRunnerArgs(scriptPath string) (string, []string) {
	var command strings.Builder
	// the expectation is that they have git-bash installed. if not it will just error out.
	shell := "C:\\Program Files\\Git\\bin\\bash.exe"
	command.WriteString("exec \"" + scriptPath + "\";")

	return shell, []string{"-lc", command.String()}
}
