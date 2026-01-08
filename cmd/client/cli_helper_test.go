package main

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// runCLI executes this package's cobra CLI in a helper subprocess so we can
// assert on commands that call os.Exit().
func runCLI(t *testing.T, args ...string) (stdoutStderr string, exitCode int) {
	t.Helper()

	cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, args...)...)
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"NO_COLOR=1",
		"CLICOLOR=0",
		"CLICOLOR_FORCE=0",
		"TERM=dumb",
	)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	if err == nil {
		return buf.String(), 0
	}

	if ee, ok := err.(*exec.ExitError); ok {
		return buf.String(), ee.ExitCode()
	}

	t.Fatalf("unexpected error running CLI: %v", err)
	return "", 0
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	// Args are: <testbin> -test.run=TestHelperProcess -- <cli args...>
	idx := -1
	for i, a := range os.Args {
		if a == "--" {
			idx = i
			break
		}
	}
	if idx == -1 {
		os.Exit(2)
	}

	cliArgs := os.Args[idx+1:]
	// Cobra prints help on empty args; we never want to run the default daemon.
	if len(cliArgs) == 0 {
		os.Exit(2)
	}

	rootCmd.SetArgs(cliArgs)
	// Keep cobra help/errors deterministic for tests.
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	if err := rootCmd.Execute(); err != nil {
		// Best-effort map: cobra errors are usually usage errors.
		msg := strings.TrimSpace(stripANSI(err.Error()))
		if msg != "" {
			_, _ = os.Stderr.WriteString(msg + "\n")
		}
		os.Exit(1)
	}
	os.Exit(0)
}

