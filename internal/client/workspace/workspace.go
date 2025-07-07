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
	appsDir            = "apps"
	logsDir            = "logs"
	datasitesDir       = "datasites"
	publicDir          = "public"
	metadataDir        = ".data"
	pathSep            = string(filepath.Separator)
	lockFile           = "syftbox.lock"
	legacyMetadataFile = ".metadata.json"
)

var (
	ErrWorkspaceLocked = errors.New("workspace locked by another process")
)

type Workspace struct {
	Owner         string
	Root          string
	AppsDir       string
	DatasitesDir  string
	MetadataDir   string
	LogsDir       string
	UserDir       string
	UserPublicDir string

	flock *flock.Flock
}

func NewWorkspace(rootDir string, user string) (*Workspace, error) {
	root, err := utils.ResolvePath(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %s: %w", rootDir, err)
	}

	lockFilePath := filepath.Join(root, metadataDir, lockFile)
	flock := flock.New(lockFilePath)

	return &Workspace{
		Owner:         user,
		Root:          root,
		AppsDir:       filepath.Join(root, appsDir),
		LogsDir:       filepath.Join(root, logsDir),
		DatasitesDir:  filepath.Join(root, datasitesDir),
		MetadataDir:   filepath.Join(root, metadataDir),
		UserDir:       filepath.Join(root, datasitesDir, user),
		UserPublicDir: filepath.Join(root, datasitesDir, user, publicDir),
		flock:         flock,
	}, nil
}

func (w *Workspace) Lock() error {
	// create a .data/syftbox.lock file so that other syftbox instances cannot access the workspace
	if err := utils.EnsureDir(w.MetadataDir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", w.MetadataDir, err)
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
	// if this process hasn't locked the workspace, then don't delete the lock file
	if !w.flock.Locked() {
		return nil
	}

	if err := w.flock.Unlock(); err != nil {
		return fmt.Errorf("failed to unlock workspace: %w", err)
	}

	return os.Remove(w.flock.Path())
}

func (w *Workspace) Setup() error {
	if w.isLegacyWorkspace() {
		// rename it to from w.Root to w.Root.old
		newPath := w.Root + ".old"
		if err := os.Rename(w.Root, newPath); err != nil {
			return fmt.Errorf("failed to move legacy workspace to %s: %w", newPath, err)
		}
		slog.Warn("legacy workspace detected. moved to " + newPath)
	}

	if err := w.Lock(); err != nil {
		return err
	}

	slog.Info("workspace", "root", w.Root)

	// Create required directories
	dirs := []string{w.AppsDir, w.MetadataDir, w.UserPublicDir}
	for _, dir := range dirs {
		if err := utils.EnsureDir(dir); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := setFolderIcon(w.Root); err != nil {
		slog.Warn("failed to set folder icon", "error", err)
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

func (w *Workspace) DatasiteRelPath(path string) (string, error) {
	relPath, err := filepath.Rel(w.DatasitesDir, path)
	if err != nil {
		return "", err
	}
	return NormPath(relPath), nil
}

func (w *Workspace) PathOwner(path string) string {
	p, _ := w.DatasiteRelPath(path)
	parts := strings.Split(p, pathSep)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func (w *Workspace) isLegacyWorkspace() bool {
	// .data is missing & plugins exists
	return utils.FileExists(filepath.Join(w.Root, legacyMetadataFile))
}

func NormPath(path string) string {
	path = filepath.Clean(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimLeft(path, "/")
	return path
}
