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
		DatasitesDir:  root,
		MetadataDir:   filepath.Join(root, metadataDir),
		UserDir:       filepath.Join(root, user),
		UserPublicDir: filepath.Join(root, user, publicDir),
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

	// Setup ACL files
	if err := w.createDefaultACL(); err != nil {
		return fmt.Errorf("failed to create default ACL: %w", err)
	}

	return nil
}

func (w *Workspace) createDefaultACL() error {
	// Create root ACL file
	if !aclspec.Exists(w.UserDir) {
		rootRuleset := aclspec.NewRuleSet(
			w.UserDir,
			aclspec.NotTerminal,
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
			aclspec.NotTerminal,
			aclspec.NewDefaultRule(aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		)
		if err := publicRuleset.Save(); err != nil {
			return fmt.Errorf("public acl create error: %w", err)
		}
	}

	return nil
}

// DatasiteAbsPath returns the absolute path to the datasite directory
func (w *Workspace) DatasiteAbsPath(relPath string) string {
	return filepath.Join(w.DatasitesDir, relPath)
}

// DatasiteRelPath returns the relative path of a datasite from the workspace's datasites directory
func (w *Workspace) DatasiteRelPath(absPath string) (string, error) {
	relPath, err := filepath.Rel(w.DatasitesDir, absPath)
	if err != nil {
		return "", err
	}
	return NormPath(relPath), nil
}

// PathOwner returns the owner of the path
func (w *Workspace) PathOwner(path string) string {
	p, _ := w.DatasiteRelPath(path)
	parts := strings.Split(p, pathSep)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func (w *Workspace) IsValidPath(path string) bool {
	return IsValidPath(path)
}

func (w *Workspace) isLegacyWorkspace() bool {
	// a .metadata.json exists
	return utils.FileExists(filepath.Join(w.Root, legacyMetadataFile))
}

// NormPath normalizes a path by cleaning it, replacing backslashes with slashes, and trimming leading slashes
func NormPath(path string) string {
	path = filepath.Clean(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimLeft(path, "/")
	return path
}
