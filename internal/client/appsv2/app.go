package appsv2

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/openmined/syftbox/internal/utils"
)

type App struct {
	*AppProcess
	info       *AppInfo
	port       int
	stdoutFile io.WriteCloser
	stderrFile io.WriteCloser
}

func NewApp(info *AppInfo, sbConfig string) (*App, error) {
	port, err := utils.GetFreePort()
	if err != nil {
		slog.Error("failed to get free port", "error", err)
		return nil, err
	}

	if !IsValidApp(info.Path) {
		return nil, fmt.Errorf("invalid app: %s", info.ID)
	}

	runScript := GetRunScript(info.Path)

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

	// Create a logs directory with

	runScript, args := buildRunnerArgs(runScript)
	proc := NewAppProcess(runScript, args...).
		SetID(info.ID).
		SetWorkingDir(info.Path).
		SetEnvs(map[string]string{
			"SYFTBOX_APP_ID":             info.ID,
			"SYFTBOX_APP_DIR":            info.Path,
			"SYFTBOX_APP_PORT":           strconv.Itoa(port),
			"SYFTBOX_ASSIGNED_PORT":      strconv.Itoa(port),
			"SYFTBOX_CLIENT_CONFIG_PATH": sbConfig,
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
	logsDir := filepath.Join(a.info.Path, "logs")
	if err := utils.EnsureDir(logsDir); err != nil {
		return fmt.Errorf("failed to create logs directory for app %s: %w", a.info.ID, err)
	}

	stdoutFile, err := os.OpenFile(filepath.Join(logsDir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create stdout log file for app %s: %w", a.info.ID, err)
	}

	stderrFile, err := os.OpenFile(filepath.Join(logsDir, "stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create stderr log file for app %s: %w", a.info.ID, err)
	}

	a.stdoutFile = stdoutFile
	a.stderrFile = stderrFile

	a.AppProcess.SetStdout(a.stdoutFile)
	a.AppProcess.SetStderr(a.stdoutFile)

	return a.AppProcess.Start()
}

func (a *App) Stop() error {
	if a.stdoutFile != nil {
		a.stdoutFile.Close()
		a.stdoutFile = nil
	}
	if a.stderrFile != nil {
		a.stderrFile.Close()
		a.stderrFile = nil
	}
	return a.AppProcess.Stop()
}
