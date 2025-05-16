package apps

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// App represents a runnable application
type App struct {
	Name    string
	Path    string
	Env     []string
	Port    string
	Process *exec.Cmd
	Cancel  context.CancelFunc
	stdout  *os.File
	stderr  *os.File
}

// NewApp creates a new App instance
func NewApp(appPath string, env []string, port string) *App {
	return &App{
		Name: filepath.Base(appPath),
		Path: appPath,
		Env:  env,
		Port: port,
	}
}

// Start launches the app's run.sh script
func (a *App) Start(ctx context.Context) error {
	// Get run script path and validate it
	runScript, err := GetRunScript(a.Path)
	if err != nil {
		return err
	}

	// Get .env file content
	envContent, err := a.GetEnvFileContent()
	if err != nil {
		return fmt.Errorf("failed to get .env file content for app %s: %w", a.Name, err)
	}

	// Parse environment variables from .env file content
	envLines := strings.Split(strings.TrimSpace(envContent), "\n")
	for _, line := range envLines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			a.Env = append(a.Env, line)
		}
	}

	// Create a cancellable context for this app
	appCtx, cancel := context.WithCancel(ctx)
	a.Cancel = cancel

	// Prepare the command to run the app
	a.Process = exec.CommandContext(appCtx, "sh", runScript)
	a.Process.Dir = a.Path

	// Set environment variables
	a.Env = append(a.Env, envContent)

	slog.Info("app env", "env", envContent)
	a.Process.Env = a.Env

	// Create a logs directory within the app directory
	logsDir := filepath.Join(a.Path, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		a.Cancel()
		return fmt.Errorf("failed to create logs directory for app %s: %w", a.Name, err)
	}

	// Create log files for stdout and stderr
	stdoutLogPath := filepath.Join(logsDir, "stdout.log")
	stderrLogPath := filepath.Join(logsDir, "stderr.log")

	stdoutFile, err := os.OpenFile(stdoutLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		a.Cancel()
		return fmt.Errorf("failed to create stdout log file for app %s: %w", a.Name, err)
	}

	stderrFile, err := os.OpenFile(stderrLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		a.Cancel()
		stdoutFile.Close()
		return fmt.Errorf("failed to create stderr log file for app %s: %w", a.Name, err)
	}

	a.stdout = stdoutFile
	a.stderr = stderrFile

	// Redirect app output to log files
	a.Process.Stdout = stdoutFile
	a.Process.Stderr = stderrFile

	// Start the process
	if err := a.Process.Start(); err != nil {
		a.Cancel()
		a.closeLogFiles()
		return fmt.Errorf("failed to start app %s: %w", a.Name, err)
	}

	if a.Port != "" {
		slog.Info("app started", "app", a.Name, "url", fmt.Sprintf("http://localhost:%s", a.Port))
	} else {
		slog.Info("app started", "app", a.Name)
	}
	return nil
}

// Stop terminates the running app
func (a *App) Stop() error {
	// Cancel the app's context to signal termination
	a.Cancel()

	// Give the app some time to gracefully shut down
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	// Create a channel to signal when the app has exited
	done := make(chan struct{})
	go func() {
		a.Process.Wait()
		close(done)
	}()

	// Wait for either the app to exit or the timeout
	select {
	case <-done:
		a.closeLogFiles()
		return nil
	case <-timer.C:
		// Force kill if graceful shutdown fails
		if err := a.Process.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill app %s: %w", a.Name, err)
		}
		slog.Info("app stop", "app", a.Name)
		a.closeLogFiles()
		return nil
	}
}

// Wait monitors the process until it exits
func (a *App) Wait() error {
	err := a.Process.Wait()
	a.closeLogFiles()
	return err
}

// closeLogFiles closes the stdout and stderr log files
func (a *App) closeLogFiles() {
	if a.stdout != nil {
		a.stdout.Close()
		a.stdout = nil
	}
	if a.stderr != nil {
		a.stderr.Close()
		a.stderr = nil
	}
}

// GetEnv returns the value of an environment variable from the app.Env map
func (a *App) GetEnv(key string) string {
	for _, env := range a.Env {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 && parts[0] == key {
			slog.Info("app env", "key", key, "value", parts[1])
			return parts[1]
		}
	}
	return ""
}

// Get .env file content
func (a *App) GetEnvFileContent() (string, error) {
	content, err := os.ReadFile(filepath.Join(a.Path, ".env"))
	if err != nil {
		return "", fmt.Errorf("failed to read .env file for app %s: %w", a.Name, err)
	}
	return string(content), nil
}

// GetRunScript returns the path to the run.sh script for an app and validates it
func GetRunScript(appPath string) (string, error) {
	runScript := filepath.Join(appPath, "run.sh")
	info, err := os.Stat(runScript)
	if err != nil {
		return "", fmt.Errorf("run script not found at %s: %w", runScript, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("run script at %s is a directory", runScript)
	}
	return runScript, nil
}

// IsValidApp checks if a directory contains a valid app (has run.sh)
func IsValidApp(path string) bool {
	_, err := GetRunScript(path)
	return err == nil
}
