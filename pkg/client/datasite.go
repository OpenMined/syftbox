package client

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/yashgorana/syftbox-go/pkg/acl"
	"github.com/yashgorana/syftbox-go/pkg/utils"
)

const (
	appsDir      = "apis"
	logsDir      = "logs"
	datasitesDir = "datasites"
	publicDir    = "public"
)

type LocalDatasite struct {
	Root          string
	DatasitesDir  string
	AppsDir       string
	LogsDir       string
	UserDir       string
	UserPublicDir string
}

func NewLocalDatasite(rootDir string, user string) (*LocalDatasite, error) {
	root, err := utils.ResolvePath(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %s: %w", rootDir, err)
	}

	return &LocalDatasite{
		Root:          root,
		AppsDir:       filepath.Join(root, appsDir),
		LogsDir:       filepath.Join(root, logsDir),
		DatasitesDir:  filepath.Join(root, datasitesDir),
		UserDir:       filepath.Join(root, datasitesDir, user),
		UserPublicDir: filepath.Join(root, datasitesDir, user, publicDir),
	}, nil
}

func (w *LocalDatasite) Bootstrap() error {
	slog.Debug("datasite bootstrap", "root", w.Root)

	// Create required directories
	dirs := []string{w.AppsDir, w.LogsDir, w.UserPublicDir}
	for _, dir := range dirs {
		if err := utils.EnsureDir(dir); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Setup ACL files
	if err := w.createDefaultAcl(); err != nil {
		return fmt.Errorf("failed to create default ACL: %w", err)
	}

	// TODO: write a .syftignore file

	return nil
}

func (w *LocalDatasite) createDefaultAcl() error {
	rootAclPath := acl.AsAclPath(w.UserDir)
	publicAclPath := acl.AsAclPath(w.UserPublicDir)

	// Create root ACL file
	if _, err := os.Stat(rootAclPath); os.IsNotExist(err) {
		rootRuleset := acl.NewRuleSet(
			acl.UnsetTerminal,
			acl.NewRule(acl.AllFiles, acl.PrivateAccess(), acl.DefaultLimits()),
		)
		if err := rootRuleset.Save(rootAclPath); err != nil {
			return fmt.Errorf("root acl create error: %w", err)
		}
		slog.Debug("datasite create", "acl", rootAclPath)
	}

	// Create public ACL file
	if _, err := os.Stat(publicAclPath); os.IsNotExist(err) {
		publicRuleset := acl.NewRuleSet(
			acl.UnsetTerminal,
			acl.NewRule(acl.AllFiles, acl.PublicReadAccess(), acl.DefaultLimits()),
		)
		if err := publicRuleset.Save(publicAclPath); err != nil {
			return fmt.Errorf("public acl create error: %w", err)
		}
		slog.Debug("datasite create", "acl", publicAclPath)
	}

	return nil
}
