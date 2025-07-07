package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	appInfoFileName = ".syftboxapp.json"
)

var (
	ErrInvalidApp = errors.New("not a valid syftbox app")
)

// AppManager handles app installation, uninstallation, and listing operations
type AppManager struct {
	AppsDir string // Directory where apps are stored
	DataDir string // Directory where internal data is stored
	flock   *flock.Flock

	installedApps   map[string]*AppInfo
	installedAppsMu sync.RWMutex
}

// NewManager creates a new Manager instance with the specified app directory
func NewManager(appDir string, internalDataDir string) *AppManager {
	flock := flock.New(filepath.Join(internalDataDir, "apps.lock"))
	return &AppManager{
		AppsDir:       appDir,
		DataDir:       internalDataDir,
		flock:         flock,
		installedApps: make(map[string]*AppInfo),
	}
}

func (a *AppManager) GetAppByID(appID string) (*AppInfo, error) {
	a.installedAppsMu.RLock()
	defer a.installedAppsMu.RUnlock()

	app, ok := a.installedApps[appID]
	if !ok {
		return nil, ErrAppNotFound
	}
	return app, nil
}

func (a *AppManager) ListApps() ([]*AppInfo, error) {
	scannedApps := make(map[string]*AppInfo)

	appDirs, err := os.ReadDir(a.AppsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*AppInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read apps dir: %w", err)
	}

	for _, dir := range appDirs {
		if dir.IsDir() || dir.Type()&os.ModeSymlink != 0 {
			fullPath := filepath.Join(a.AppsDir, dir.Name())

			appInfo, err := a.loadAppInfoFromPath(fullPath)
			if err != nil {
				slog.Warn("failed to load app info", "path", fullPath, "error", err)
				continue
			}
			scannedApps[appInfo.ID] = appInfo
		}
	}

	a.installedAppsMu.Lock()
	a.installedApps = scannedApps
	a.installedAppsMu.Unlock()

	return slices.Collect(maps.Values(scannedApps)), nil
}

// UninstallApp uninstalls an app from a given uri.
// The uri can be a local path or a remote url or app id
func (a *AppManager) UninstallApp(uri string) (string, error) {
	if err := a.flock.Lock(); err != nil {
		return "", fmt.Errorf("failed to acquire lock for uninstall: %w", err)
	}
	defer a.flock.Unlock()

	var targetDir string
	var appID string

	switch {
	case utils.DirExists(uri):
		// URI points directly to an app directory on disk
		targetDir = uri
		info, err := a.loadAppInfoFromPath(targetDir)
		if err != nil {
			return "", fmt.Errorf("failed to load app info: %w", err)
		}
		appID = info.ID

	case utils.DirExists(a.getAppDir(uri)):
		// URI is an app ID already installed under AppsDir
		targetDir = a.getAppDir(uri)
		appID = uri

	case utils.IsValidURL(uri):
		// URI is a repository URL; derive the app ID and installation directory
		parsed, err := parseRepoURL(uri)
		if err != nil {
			return "", err
		}
		appID = appIDFromURL(parsed)
		targetDir = a.getAppDir(appID)

	default:
		return "", fmt.Errorf("no app found for %q", uri)
	}

	// 1) The directory must correspond to a valid syftbox app
	if !IsValidApp(targetDir) {
		return "", fmt.Errorf("no app found for %q", uri)
	}

	if !utils.DirExists(targetDir) {
		return "", fmt.Errorf("no app found for %q", uri)
	}

	// Remove the application directory
	if err := os.RemoveAll(targetDir); err != nil {
		return "", fmt.Errorf("failed to remove app: %w", err)
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

	if opts.UseGit && systemGitAvailable() {
		if err := installFromGit(ctx, opts.URI, appDir, opts.Branch, opts.Tag, opts.Commit); err != nil {
			return nil, fmt.Errorf("failed to install from git: %w", err)
		}
	} else {
		// Fallback to archive download/extract
		if err := installFromArchive(ctx, parsedURL, &opts, appDir); err != nil {
			return nil, fmt.Errorf("failed to install from archive: %w", err)
		}
	}

	// if not a valid app, return an error
	if !IsValidApp(appDir) {
		if err := os.RemoveAll(appDir); err != nil {
			return nil, fmt.Errorf("failed to remove app: %w", err)
		}
		return nil, ErrInvalidApp
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
		return nil, ErrInvalidApp
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
	metadataPath := filepath.Join(app.Path, appInfoFileName)
	metadata, err := json.Marshal(app)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return os.WriteFile(metadataPath, metadata, 0o644)
}

func (a *AppManager) loadAppInfoFromPath(appPath string) (*AppInfo, error) {
	if !IsValidApp(appPath) {
		return nil, ErrInvalidApp
	}

	metadataPath := filepath.Join(appPath, appInfoFileName)
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// metadata doesn't exist, consider it local app
			return &AppInfo{
				ID:        appIDFromPath(appPath),
				Name:      appNameFromPath(appPath),
				Path:      appPath,
				Source:    AppSourceLocalDir,
				SourceURI: appPath,
			}, nil
		}
		return nil, fmt.Errorf("failed to read app info from %q: %w", metadataPath, err)
	}

	var app AppInfo
	if err := json.Unmarshal(metadata, &app); err != nil {
		return nil, fmt.Errorf("failed to unmarshal app info: %w", err)
	}
	return &app, nil

}

// getAppDir returns the directory for a given app URI (id, dirname)
func (a *AppManager) getAppDir(uri string) string {
	return filepath.Join(a.AppsDir, uri)
}
