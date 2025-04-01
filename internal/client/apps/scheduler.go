package apps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const scanInterval = 5 * time.Second

// App represents a runnable application
type App struct {
	Name    string
	Path    string
	Process *exec.Cmd
	Cancel  context.CancelFunc
}

// AppScheduler watches a directory for apps and manages their lifecycle
type AppScheduler struct {
	appDir      string
	apps        map[string]*App
	mu          sync.RWMutex
	stopWatcher context.CancelFunc
}

// NewScheduler creates a new AppScheduler for the given directory
func NewScheduler(appDir string) *AppScheduler {
	return &AppScheduler{
		appDir: appDir,
		apps:   make(map[string]*App),
	}
}

// Start initializes the scheduler and begins monitoring for apps
func (s *AppScheduler) Start(ctx context.Context) error {
	slog.Info("Starting app scheduler", "appdir", s.appDir)

	// Create the app directory if it doesn't exist
	if err := os.MkdirAll(s.appDir, 0755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Create a context for the watcher
	watchCtx, cancel := context.WithCancel(ctx)
	s.stopWatcher = cancel

	// Start the periodic scanning
	ticker := time.NewTicker(scanInterval)

	go func() {
		defer ticker.Stop()

		// Initial scan
		s.scanDirectoryForApps(watchCtx)

		// Periodic scans
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				s.scanDirectoryForApps(watchCtx)
			}
		}
	}()

	// Wait for the context to be cancelled
	go func() {
		<-ctx.Done()
		s.Shutdown()
	}()

	return nil
}

// Shutdown stops all running apps and cleans up resources
func (s *AppScheduler) Shutdown() {
	slog.Info("Shutting down app scheduler")
	// Stop watching for new apps
	if s.stopWatcher != nil {
		s.stopWatcher()
	}

	// Get a list of all running apps
	s.mu.RLock()
	apps := make([]string, 0, len(s.apps))
	for name := range s.apps {
		apps = append(apps, name)
	}
	s.mu.RUnlock()

	// Stop each app
	for _, name := range apps {
		if err := s.stopApp(name); err != nil {
			slog.Error("Error stopping app during shutdown", "app", name, "error", err)
		}
	}
}

// ListRunningApps returns a list of currently running apps
func (s *AppScheduler) ListRunningApps() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	apps := make([]string, 0, len(s.apps))
	for name := range s.apps {
		apps = append(apps, name)
	}

	return apps
}

// startApp launches the app's run.sh script
func (s *AppScheduler) startApp(ctx context.Context, appPath string) error {
	appName := filepath.Base(appPath)

	s.mu.RLock()
	_, exists := s.apps[appName]
	s.mu.RUnlock()

	if exists {
		return fmt.Errorf("app %s is already running", appName)
	}

	if !isValidApp(appPath) {
		return fmt.Errorf("not a valid app at %s", appPath)
	}

	// Create a cancellable context for this app
	appCtx, cancel := context.WithCancel(ctx)

	// Prepare the command to run the app
	runScript := filepath.Join(appPath, "run.sh")
	cmd := exec.CommandContext(appCtx, runScript)
	cmd.Dir = appPath

	// Create a logs directory within the app directory
	logsDir := filepath.Join(appPath, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		cancel()
		return fmt.Errorf("failed to create logs directory for app %s: %w", appName, err)
	}

	// Create log files for stdout and stderr
	stdoutLogPath := filepath.Join(logsDir, "stdout.log")
	stderrLogPath := filepath.Join(logsDir, "stderr.log")

	stdoutFile, err := os.OpenFile(stdoutLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdout log file for app %s: %w", appName, err)
	}

	stderrFile, err := os.OpenFile(stderrLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		cancel()
		stdoutFile.Close()
		return fmt.Errorf("failed to create stderr log file for app %s: %w", appName, err)
	}

	// Redirect app output to log files
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	// Create an app entry
	app := &App{
		Name:    appName,
		Path:    appPath,
		Process: cmd,
		Cancel:  cancel,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		cancel()
		stdoutFile.Close()
		stderrFile.Close()
		return fmt.Errorf("failed to start app %s: %w", appName, err)
	}

	// Store the running app
	s.mu.Lock()
	s.apps[appName] = app
	s.mu.Unlock()

	// Monitor the process in a goroutine
	go func() {
		err := cmd.Wait()

		// Close log files
		stdoutFile.Close()
		stderrFile.Close()

		s.mu.Lock()
		delete(s.apps, appName)
		s.mu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("App exited with error", "app", appName, "error", err)
		} else {
			slog.Info("App exited successfully", "app", appName)
		}
	}()

	slog.Info("Started app", "app", appName)
	return nil
}

// stopApp terminates a running app
func (s *AppScheduler) stopApp(appName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	app, exists := s.apps[appName]
	if !exists {
		return fmt.Errorf("app %s is not running", appName)
	}

	// Cancel the app's context to signal termination
	app.Cancel()

	// Give the app some time to gracefully shut down
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	// Create a channel to signal when the app has exited
	done := make(chan struct{})
	go func() {
		app.Process.Wait()
		close(done)
	}()

	// Wait for either the app to exit or the timeout
	select {
	case <-done:
		delete(s.apps, appName)
		return nil
	case <-timer.C:
		// Force kill if graceful shutdown fails
		if err := app.Process.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill app %s: %w", appName, err)
		}
		slog.Info("app killed", "app", appName)
		delete(s.apps, appName)
		return nil
	}
}

// scanDirectoryForApps checks for new and removed apps
func (s *AppScheduler) scanDirectoryForApps(ctx context.Context) {
	// Get all directories in appDir
	entries, err := os.ReadDir(s.appDir)
	if err != nil {
		slog.Error("Failed to scan app directory", "error", err)
		return
	}

	// Track apps we find in this scan
	foundApps := make(map[string]bool)

	// Check for new apps
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		appPath := filepath.Join(s.appDir, appName)
		foundApps[appName] = true

		// Check if app is valid and not already running
		if isValidApp(appPath) {
			s.mu.RLock()
			_, exists := s.apps[appName]
			s.mu.RUnlock()

			if !exists {
				// Start the app
				if err := s.startApp(ctx, appPath); err != nil {
					slog.Error("Failed to start app during scan", "app", appName, "error", err)
				}
			}
		}
	}

	// Check for removed apps
	s.mu.RLock()
	runningApps := make([]string, 0, len(s.apps))
	for appName := range s.apps {
		runningApps = append(runningApps, appName)
	}
	s.mu.RUnlock()

	// Stop any apps that no longer exist
	for _, appName := range runningApps {
		appPath := filepath.Join(s.appDir, appName)
		if !foundApps[appName] || !isValidApp(appPath) {
			if err := s.stopApp(appName); err != nil {
				slog.Error("Failed to stop removed app during scan", "app", appName, "error", err)
			}
		}
	}
}

// isValidApp checks if a directory contains a valid app (has run.sh)
func isValidApp(path string) bool {
	runScript := filepath.Join(path, "run.sh")
	info, err := os.Stat(runScript)
	return err == nil && !info.IsDir() && (info.Mode()&0111 != 0) // Check if run.sh exists and is executable
}
