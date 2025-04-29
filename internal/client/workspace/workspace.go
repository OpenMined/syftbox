package workspace

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	appsDir      = "apps"
	logsDir      = "logs"
	datasitesDir = "datasites"
	publicDir    = "public"
	dataDir      = ".data"
	pathSep      = string(filepath.Separator)
	lockFile     = "syftbox.lock"
)

var (
	ErrWorkspaceLocked = errors.New("workspace locked by another process")
)

type Workspace struct {
	Owner           string
	Root            string
	AppsDir         string
	DatasitesDir    string
	InternalDataDir string
	LogsDir         string
	UserDir         string
	UserPublicDir   string

	flock *flock.Flock
}

func NewWorkspace(rootDir string, user string) (*Workspace, error) {
	root, err := utils.ResolvePath(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %s: %w", rootDir, err)
	}

	lockFilePath := filepath.Join(root, dataDir, lockFile)
	flock := flock.New(lockFilePath)

	return &Workspace{
		Owner:           user,
		Root:            root,
		AppsDir:         filepath.Join(root, appsDir),
		LogsDir:         filepath.Join(root, logsDir),
		DatasitesDir:    filepath.Join(root, datasitesDir),
		InternalDataDir: filepath.Join(root, dataDir),
		UserDir:         filepath.Join(root, datasitesDir, user),
		UserPublicDir:   filepath.Join(root, datasitesDir, user, publicDir),
		flock:           flock,
	}, nil
}

func (w *Workspace) Lock() error {
	// create a .data/syftbox.lock file so that other syftbox instances cannot access the workspace
	if err := utils.EnsureDir(w.InternalDataDir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", w.InternalDataDir, err)
	}

	locked, err := w.flock.TryLock()
	if err != nil {
		return fmt.Errorf("failed to lock workspace: %w", err)
	}
	if !locked {
		return ErrWorkspaceLocked
	}

	return nil
}

func (w *Workspace) Unlock() error {
	if err := w.flock.Unlock(); err != nil {
		return fmt.Errorf("failed to unlock workspace: %w", err)
	}

	return os.Remove(w.flock.Path())
}

func (w *Workspace) Setup() error {
	if w.isLegacyWorkspace() {
		slog.Info("legacy workspace detected, cleaning up")
		if err := os.RemoveAll(w.Root); err != nil {
			return fmt.Errorf("failed to clean up legacy workspace: %w", err)
		}
	}

	if err := w.Lock(); err != nil {
		return err
	}

	slog.Info("workspace", "root", w.Root)

	// Create required directories
	dirs := []string{w.AppsDir, w.InternalDataDir, w.UserPublicDir}
	for _, dir := range dirs {
		if err := utils.EnsureDir(dir); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// TODO: write a .syftignore file

	// Setup ACL files
	if err := w.createDefaultAcl(); err != nil {
		return fmt.Errorf("failed to create default ACL: %w", err)
	}

	return nil
}

func (w *Workspace) createDefaultAcl() error {
	// Create root ACL file
	if !aclspec.Exists(w.UserDir) {
		rootRuleset := aclspec.NewRuleSet(
			w.UserDir,
			aclspec.UnsetTerminal,
			aclspec.NewDefaultRule(aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		)
		if err := rootRuleset.Save(); err != nil {
			return fmt.Errorf("root acl create error: %w", err)
		}
	}

	// Create public ACL file
	if !aclspec.Exists(w.UserPublicDir) {
		publicRuleset := aclspec.NewRuleSet(
			w.UserPublicDir,
			aclspec.UnsetTerminal,
			aclspec.NewDefaultRule(aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		)
		if err := publicRuleset.Save(); err != nil {
			return fmt.Errorf("public acl create error: %w", err)
		}
	}

	return nil
}

func (w *Workspace) DatasiteAbsPath(path string) string {
	return filepath.Join(w.DatasitesDir, path)
}

func (w *Workspace) DatasiteRelPath(path string) string {
	path, _ = utils.ResolvePath(path)
	path = strings.Replace(path, w.DatasitesDir, "", 1)
	return strings.ReplaceAll(strings.TrimLeft(filepath.Clean(path), pathSep), pathSep, "/")
}

func (w *Workspace) PathOwner(path string) string {
	p := w.DatasiteRelPath(path)
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func (w *Workspace) isLegacyWorkspace() bool {
	return utils.DirExists(filepath.Join(w.Root, "plugins"))
}
