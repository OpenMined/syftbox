package appsv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	appInfoFileName = ".syftboxapp.json"
)

// AppManager handles app installation, uninstallation, and listing operations
type AppManager struct {
	AppsDir string // Directory where apps are stored
	DataDir string // Directory where internal data is stored
	flock   *flock.Flock
}

// NewManager creates a new Manager instance with the specified app directory
func NewManager(appDir string, internalDataDir string) *AppManager {
	flock := flock.New(filepath.Join(internalDataDir, "apps.lock"))
	return &AppManager{
		AppsDir: appDir,
		DataDir: internalDataDir,
		flock:   flock,
	}
}

func (a *AppManager) ListApps() ([]*AppInfo, error) {
	apps := make([]*AppInfo, 0)

	appDirs, err := os.ReadDir(a.AppsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return apps, nil
		}
		return nil, fmt.Errorf("failed to read apps dir: %w", err)
	}

	for _, dir := range appDirs {
		if dir.IsDir() || dir.Type()&os.ModeSymlink != 0 {
			appID := dir.Name()
			appDir := a.getAppDir(appID)

			if !IsValidApp(appDir) {
				continue
			}

			appInfo, err := a.loadAppInfo(appID)
			if err != nil && errors.Is(err, os.ErrNotExist) { // if the app info doesn't exist, create a new one
				appName := appNameFromPath(appDir)
				appDirInfo, err := os.Stat(appDir)
				if err != nil {
					slog.Warn("failed to stat app dir", "error", err)
					continue
				}
				appInfo = &AppInfo{
					ID:          appID,
					Name:        appName,
					Path:        appDir,
					Source:      AppSourceLocalDir,
					SourceURI:   appDir,
					InstalledOn: appDirInfo.ModTime(),
				}
			} else if err != nil {
				slog.Warn("failed to load app info", "error", err)
				continue
			}
			apps = append(apps, appInfo)
		}
	}

	return apps, nil
}

// UninstallApp uninstalls an app from a given uri.
// The uri can be a local path or a remote url or app id
func (a *AppManager) UninstallApp(uri string) (string, error) {
	var targetDir string
	var appID string

	if err := a.flock.Lock(); err != nil {
		return "", fmt.Errorf("failed to acquire lock for uninstall: %w", err)
	}
	defer a.flock.Unlock()

	if utils.DirExists(uri) {
		targetDir = uri
	} else if utils.DirExists(a.getAppDir(uri)) {
		targetDir = a.getAppDir(uri)
		appID = uri
	} else if utils.IsValidURL(uri) {
		parsedURL, err := parseRepoURL(uri)
		if err != nil {
			return "", err
		}
		appID = appIDFromURL(parsedURL)
		targetDir = a.getAppDir(appID)
	} else {
		return "", fmt.Errorf("no app found for %q", uri)
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return "", fmt.Errorf("failed to remove app: %w", err)
	}

	if err := a.deleteAppInfo(appID); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to delete app info: %w", err)
	}

	return appID, nil
}

func (a *AppManager) InstallApp(ctx context.Context, opts AppInstallOpts) (*AppInfo, error) {
	if err := utils.EnsureDir(a.AppsDir); err != nil {
		return nil, fmt.Errorf("failed to create dir %q: %w", a.AppsDir, err)
	}

	if err := utils.EnsureDir(a.DataDir); err != nil {
		return nil, fmt.Errorf("failed to create dir %q: %w", a.DataDir, err)
	}

	// only one app install at a time
	if err := a.flock.Lock(); err != nil {
		return nil, fmt.Errorf("failed to lock apps dir: %w", err)
	}
	defer a.flock.Unlock()

	// install from path or url
	if utils.DirExists(opts.URI) {
		return a.installFromPath(ctx, opts)
	} else if utils.IsValidURL(opts.URI) {
		return a.installFromURL(ctx, opts)
	}

	return nil, fmt.Errorf("invalid url or path %q", opts.URI)
}

// installs an app from a git repository URL
func (a *AppManager) installFromURL(ctx context.Context, opts AppInstallOpts) (*AppInfo, error) {
	parsedURL, err := parseRepoURL(opts.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	appID := appIDFromURL(parsedURL)
	appName := appNameFromURL(parsedURL)
	appDir := a.getAppDir(appID)

	if err := a.prepareInstallLocation(appDir, opts.Force); err != nil {
		return nil, err
	}

	if opts.UseGit {
		if err := installFromGit(ctx, opts.URI, appDir, opts.Branch, opts.Tag, opts.Commit); err != nil {
			return nil, fmt.Errorf("failed to install from git: %w", err)
		}
	} else {
		// Fallback to archive download/extract
		if err := installFromArchive(ctx, parsedURL, &opts, appDir); err != nil {
			return nil, fmt.Errorf("failed to install from archive: %w", err)
		}
	}

	appInfo := &AppInfo{
		ID:          appID,
		Name:        appName,
		Path:        appDir,
		Source:      AppSourceGit,
		SourceURI:   opts.URI,
		Branch:      opts.Branch,
		Tag:         opts.Tag,
		Commit:      opts.Commit,
		InstalledOn: time.Now(),
	}

	if err := a.saveAppInfo(appInfo); err != nil {
		return nil, fmt.Errorf("failed to save app metadata: %w", err)
	}

	return appInfo, nil
}

// installs an app from a local path (will be a symlink)
func (a *AppManager) installFromPath(_ context.Context, opts AppInstallOpts) (*AppInfo, error) {
	fullPath, err := utils.ResolvePath(opts.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	if !IsValidApp(fullPath) {
		return nil, fmt.Errorf("invalid app: %s. missing run.sh", fullPath)
	}

	// create a symlink from opts.Path to the apps directory
	appId := appIDFromPath(fullPath)
	appName := appNameFromPath(fullPath)
	appDir := a.getAppDir(appId)

	if err := a.prepareInstallLocation(appDir, opts.Force); err != nil {
		return nil, err
	}

	if err := os.Symlink(fullPath, appDir); err != nil {
		return nil, fmt.Errorf("failed to create symlink: %w", err)
	}

	appInfo := &AppInfo{
		ID:          appId,
		Name:        appName,
		Path:        appDir,
		Source:      AppSourceLocalDir,
		SourceURI:   fullPath,
		InstalledOn: time.Now(),
	}

	if err := a.saveAppInfo(appInfo); err != nil {
		return nil, fmt.Errorf("failed to save app metadata: %w", err)
	}

	return appInfo, nil
}

func (a *AppManager) prepareInstallLocation(installDir string, force bool) error {
	if !utils.DirExists(installDir) {
		return nil
	}

	// install dir exists, remove it if force is true
	if force {
		if err := os.RemoveAll(installDir); err != nil {
			return fmt.Errorf("failed to remove app: %w", err)
		}
	} else {
		return fmt.Errorf("app already exists at %q", installDir)
	}

	return nil
}

// saves the app metadata to the data directory
func (a *AppManager) saveAppInfo(app *AppInfo) error {
	metadataPath := a.getAppInfoPath(app.ID)
	metadata, err := json.Marshal(app)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return os.WriteFile(metadataPath, metadata, 0o644)
}

// loads the app metadata from the data directory
func (a *AppManager) loadAppInfo(appID string) (*AppInfo, error) {
	metadataPath := a.getAppInfoPath(appID)
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var app AppInfo
	if err := json.Unmarshal(metadata, &app); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}
	return &app, nil
}

func (a *AppManager) deleteAppInfo(appID string) error {
	metadataPath := a.getAppInfoPath(appID)
	if err := os.Remove(metadataPath); err != nil {
		return fmt.Errorf("failed to remove metadata: %w", err)
	}
	return nil
}

func (a *AppManager) getAppInfoPath(appID string) string {
	return filepath.Join(a.getAppDir(appID), appInfoFileName)
}

func (a *AppManager) getAppDir(appID string) string {
	return filepath.Join(a.AppsDir, appID)
}
