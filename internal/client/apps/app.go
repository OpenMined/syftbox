package apps

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
)

type App struct {
	*AppProcess
	info   *AppInfo
	port   int
	appLog io.WriteCloser
}

func NewApp(info *AppInfo, configPath string) (*App, error) {
	port, err := utils.GetFreePort()
	if err != nil {
		slog.Error("failed to get free port", "error", err)
		return nil, err
	}

	if !IsValidApp(info.Path) {
		return nil, fmt.Errorf("invalid app: %s", info.ID)
	}

	runScript := info.RunScriptPath()

	// chmod +x the scripts
	// get perms of the script
	perms, err := os.Stat(runScript)
	if err != nil {
		slog.Error("failed to get perms of script", "error", err)
		return nil, err
	}

	// set +x
	if err := os.Chmod(runScript, perms.Mode()|0111); err != nil {
		slog.Error("failed to make script executable", "error", err)
		return nil, err
	}

	// create the run script
	runScript, args := buildRunnerArgs(runScript)

	// custom path env
	customPathEnv := createCustomPathEnv(
		systemPathWithoutVenv(),
		os.Getenv("SYFTBOX_DESKTOP_BINARIES_PATH"),
		os.Getenv("SYFTBOX_EXTRA_PATH"),
	)

	// create the app process
	proc := NewAppProcess(runScript, args...).
		SetID(info.ID).
		SetWorkingDir(info.Path).
		SetEnvs(map[string]string{
			"SYFTBOX_APP_ID":             info.ID,
			"SYFTBOX_APP_DIR":            info.Path,
			"SYFTBOX_APP_PORT":           strconv.Itoa(port),
			"SYFTBOX_ASSIGNED_PORT":      strconv.Itoa(port),
			"SYFTBOX_CLIENT_CONFIG_PATH": configPath,
			"PATH":                       customPathEnv,
		})

	return &App{
		AppProcess: proc,
		info:       info,
		port:       port,
	}, nil
}

func (a *App) Info() *AppInfo {
	a.AppProcess.procMu.RLock()
	defer a.AppProcess.procMu.RUnlock()

	return a.info
}

func (a *App) Start() error {
	// in the app directory
	logsDir := a.info.LogsDir()

	// clean up old logs
	if err := os.RemoveAll(logsDir); err != nil {
		slog.Debug("failed to remove logs directory for app", "app", a.info.ID, "error", err)
	}

	if err := utils.EnsureDir(logsDir); err != nil {
		return fmt.Errorf("failed to create logs directory for app %s: %w", a.info.ID, err)
	}

	appLog, err := os.OpenFile(filepath.Join(logsDir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create stdout log file for app %s: %w", a.info.ID, err)
	}

	a.appLog = utils.NewLogInterceptor(appLog)
	a.AppProcess.SetStdout(a.appLog)
	a.AppProcess.SetStderr(a.appLog)

	return a.AppProcess.Start()
}

func (a *App) Stop() error {
	if a.appLog != nil {
		a.appLog.Close()
		a.appLog = nil
	}
	return a.AppProcess.Stop()
}

func createCustomPathEnv(customPaths ...string) string {
	final := make([]string, 0)

	for _, path := range customPaths {
		if path != "" {
			final = append(final, path)
		}
	}

	return strings.Join(final, string(os.PathListSeparator))
}

func systemPathWithoutVenv() string {
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
