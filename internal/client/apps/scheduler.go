package apps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/utils"
)

const (
	scanInterval = 2 * time.Second
)

// AppScheduler watches a directory for apps and manages their lifecycle
type AppScheduler struct {
	appDir        string
	configPath    string
	apps          map[string]*App
	mu            sync.RWMutex
	stopWatcher   context.CancelFunc
	subprocessEnv []string
}

// NewScheduler creates a new AppScheduler for the given directory
func NewScheduler(appDir string, configPath string) *AppScheduler {
	// Build clean environment upfront
	procEnvs := []string{}

	// drop PATH and VIRTUAL_ENV from the environment
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "PATH=") || strings.HasPrefix(env, "VIRTUAL_ENV=") {
			continue
		}
		procEnvs = append(procEnvs, env)
	}

	pathEnv := pathWithoutVenv()
	procEnvs = append(procEnvs, fmt.Sprintf("PATH=%s", pathEnv))
	procEnvs = append(procEnvs, fmt.Sprintf("SYFTBOX_CLIENT_CONFIG_PATH=%s", configPath))

	return &AppScheduler{
		appDir:        appDir,
		configPath:    configPath,
		apps:          make(map[string]*App),
		subprocessEnv: procEnvs,
	}
}

// Start initializes the scheduler and begins monitoring for apps
func (s *AppScheduler) Start(ctx context.Context) error {

	slog.Info("app scheduler start", "appdir", s.appDir)

	// Create the app directory if it doesn't exist
	if err := os.MkdirAll(s.appDir, 0o755); err != nil {
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
			slog.Error("app scheduler shutdown", "app", name, "error", err)
		}
	}
	slog.Info("app scheduler shutdown")
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

// startApp launches the app
func (s *AppScheduler) startApp(ctx context.Context, appPath string) error {
	appName := filepath.Base(appPath)

	s.mu.RLock()
	_, exists := s.apps[appName]
	s.mu.RUnlock()

	if exists {
		return fmt.Errorf("app %s is already running", appName)
	}

	if !IsValidApp(appPath) {
		return fmt.Errorf("not a valid app at %s", appPath)
	}

	port, err := utils.GetFreePort()
	if err != nil {
		port = -1
	}

	strPort := strconv.Itoa(port)

	procEnvs := make([]string, len(s.subprocessEnv))
	copy(procEnvs, s.subprocessEnv)
	if strPort != "" {
		procEnvs = append(procEnvs, fmt.Sprintf("SYFTBOX_ASSIGNED_PORT=%s", strPort))
	} else {
		slog.Error("failed to get free port")
	}

	// Create a new app instance
	app := NewApp(appPath, procEnvs, strPort)

	// Start the app
	if err := app.Start(ctx); err != nil {
		return err
	}

	// Store the running app
	s.mu.Lock()
	s.apps[appName] = app
	s.mu.Unlock()

	// Monitor the process in a goroutine
	go func() {
		if err := app.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("app scheduler error", "app", appName, "error", err)
		}
		s.mu.Lock()
		delete(s.apps, appName)
		s.mu.Unlock()

	}()

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

	// Stop the app
	if err := app.Stop(); err != nil {
		return err
	}

	delete(s.apps, appName)
	return nil
}

// scanDirectoryForApps checks for new and removed apps
func (s *AppScheduler) scanDirectoryForApps(ctx context.Context) {
	// Get all directories in appDir
	entries, err := os.ReadDir(s.appDir)
	if err != nil {
		slog.Error("app scheduler scan", "error", err)
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
		if IsValidApp(appPath) {
			s.mu.RLock()
			_, exists := s.apps[appName]
			s.mu.RUnlock()

			if !exists {
				// Start the app
				if err := s.startApp(ctx, appPath); err != nil {
					slog.Error("app scheduler scan", "app", appName, "error", err)
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
		if !foundApps[appName] || !IsValidApp(appPath) {
			if err := s.stopApp(appName); err != nil {
				slog.Error("app scheduler scan", "app", appName, "error", err)
			}
		}
	}
}

// pathWithoutVenv drops any venv paths from the PATH environment variable
func pathWithoutVenv() string {
	envPath := os.Getenv("PATH")
	if envPath == "" {
		return envPath
	}

	// list for directories commonly associated with Python virtual environments.
	excludeList := []string{
		filepath.Join("env", "bin"),
		filepath.Join("env", "Scripts"),
		"conda",
		".virtualenvs",
		"pyenv",
	}

	// activated venv will have VIRTUAL_ENV and VIRTUAL_ENV/bin in PATH
	// so we add it to the hints
	if envVenv := os.Getenv("VIRTUAL_ENV"); envVenv != "" {
		excludeList = append(excludeList, envVenv)
	}

	// Split the PATH and filter out entries that match our hints.
	pathSegments := strings.Split(envPath, string(os.PathListSeparator))
	cleanedSegments := make([]string, 0, len(pathSegments))

	for _, segment := range pathSegments {
		lowerSegment := strings.ToLower(segment)
		exclude := false

		for _, hint := range excludeList {
			if strings.Contains(lowerSegment, strings.ToLower(hint)) {
				exclude = true
				break
			}
		}
		if !exclude {
			cleanedSegments = append(cleanedSegments, segment)
		}
	}

	// Rejoin the filtered segments into a single PATH string.
	return strings.Join(cleanedSegments, string(os.PathListSeparator))
}
