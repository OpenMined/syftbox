package apps

import (
	"archive/zip"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"resty.dev/v3"
)

// AppManager handles app installation, uninstallation, and listing operations
type AppManager struct {
	AppsDir string // Directory where apps are stored
}

// RepoOpts contains options for repository installation
type RepoOpts struct {
	Branch string // Git branch to install
	Tag    string // Git tag to install
	Commit string // Git commit hash to install
}

// NewManager creates a new Manager instance with the specified app directory
func NewManager(appDir string) *AppManager {
	return &AppManager{AppsDir: appDir}
}

// ListApps returns a list of all installed app names
func (a *AppManager) ListApps() ([]string, error) {
	apps, err := os.ReadDir(a.AppsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read apps dir: %w", err)
	}

	appNames := make([]string, 0, len(apps))
	for _, app := range apps {
		if app.IsDir() {
			appNames = append(appNames, app.Name())
		}
	}

	return appNames, nil
}

// UninstallApp removes the specified app from the apps directory
func (a *AppManager) UninstallApp(appName string) error {
	// TODO stop the app if it is running, otherwise app directory doesn't get deleted properly across OS
	appDir := filepath.Join(a.AppsDir, appName)
	if err := os.RemoveAll(appDir); err != nil {
		return fmt.Errorf("failed to remove app: %w", err)
	}
	return nil
}

// InstallRepo installs an app from a git repository URL
// If force is true, it will remove any existing app with the same name
// Returns the installed App and any error encountered
func (a *AppManager) InstallRepo(repoUrl string, opts *RepoOpts, force bool) (*App, error) {
	// if url is not a valid git url, return an error
	parsed, err := url.Parse(repoUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, ".git")

	appName := filepath.Base(parsed.Path)
	appDir := filepath.Join(a.AppsDir, appName)

	if force {
		if err := os.RemoveAll(appDir); err != nil {
			return nil, fmt.Errorf("failed to remove app: %w", err)
		}
	}

	if exists(appDir) {
		return nil, fmt.Errorf("app already exists: %s", appDir)
	}

	archiveUrl, err := a.getArchiveUrl(parsed, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get archive url: %w", err)
	}

	archivePath, err := downloadFile(archiveUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to download archive: %w", err)
	}

	if err := extractZip(archivePath, appDir); err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	// Clean up the downloaded file
	if err := os.Remove(archivePath); err != nil {
		fmt.Println("failed to remove downloaded archive:", err)
	}

	return &App{
		Name: appName,
		Path: appDir,
	}, nil
}

// InstallPath creates a symlink to a local directory in the apps directory
// If force is true, it will remove any existing app with the same name
func (a *AppManager) InstallPath(path string, force bool) error {
	target := filepath.Join(a.AppsDir, filepath.Base(path))

	if force {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("failed to remove app: %w", err)
		}
	}

	if exists(target) {
		return fmt.Errorf("app already exists: %s", target)
	}

	// create a symlink
	if err := os.Symlink(path, target); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// getArchiveUrl returns the URL for downloading the repository archive
// based on the repository host and options provided
func (a *AppManager) getArchiveUrl(repoUrl *url.URL, opts *RepoOpts) (string, error) {
	switch repoUrl.Host {
	case "github.com":
		return a.githubArchiveUrl(repoUrl, opts)
	case "gitlab.com":
		return a.gitlabArchiveUrl(repoUrl, opts)
	default:
		return "", fmt.Errorf("unsupported host: %s", repoUrl.Host)
	}
}

// githubArchiveUrl generates a GitHub archive URL based on the repository and options
func (a *AppManager) githubArchiveUrl(repoUrl *url.URL, opts *RepoOpts) (string, error) {
	// github url scheme. supports zip, tar.gz
	// https://github.com/OpenMined/syft/archive/refs/heads/main.tar.gz
	// https://github.com/OpenMined/syft/archive/refs/tags/0.3.5.tar.gz
	// https://github.com/OpenMined/syft/archive/6eca36e8e46e64f557eb7ad344bd2a6be56d503e.tar.gz

	if opts.Branch != "" {
		return fmt.Sprintf("%s/archive/refs/heads/%s.zip", repoUrl.String(), opts.Branch), nil
	}

	if opts.Tag != "" {
		return fmt.Sprintf("%s/archive/refs/tags/%s.zip", repoUrl.String(), opts.Tag), nil
	}

	if opts.Commit != "" {
		return fmt.Sprintf("%s/archive/%s.zip", repoUrl.String(), opts.Commit), nil
	}

	return "", fmt.Errorf("no branch, tag or commit provided")
}

// gitlabArchiveUrl generates a GitLab archive URL based on the repository and options
func (a *AppManager) gitlabArchiveUrl(repoUrl *url.URL, opts *RepoOpts) (string, error) {
	// gitlab url scheme. supports zip, tar.gz
	// https://gitlab.com/gitlab-org/gitlab-runner/-/archive/main/archive.zip
	// https://gitlab.com/gitlab-org/gitlab-runner/-/archive/1dd26e1beea4eea6610ecd8cee97667ad6498145/archive.zip
	// https://gitlab.com/gitlab-org/gitlab-runner/-/archive/v17.10.1/archive.zip

	if opts.Branch != "" {
		return fmt.Sprintf("%s/-/archive/%s/archive.zip", repoUrl.String(), opts.Branch), nil
	}

	if opts.Tag != "" {
		return fmt.Sprintf("%s/-/archive/%s/archive.zip", repoUrl.String(), opts.Tag), nil
	}

	if opts.Commit != "" {
		return fmt.Sprintf("%s/-/archive/%s/archive.zip", repoUrl.String(), opts.Commit), nil
	}

	return "", fmt.Errorf("no branch, tag or commit provided")
}

// downloadFile downloads a file from the given URL and returns the path to the downloaded file
func downloadFile(url string) (string, error) {
	client := resty.New()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "app-*.zip")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Download the file
	resp, err := client.R().
		SetOutputFileName(tmpFile.Name()).
		Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}

	if !resp.IsSuccess() {
		return "", fmt.Errorf("failed to download file: status code %d", resp.StatusCode())
	}

	return tmpFile.Name(), nil
}

// extractZip extracts a .zip file to the target directory
func extractZip(zipPath, dst string) error {
	// Open the zip file
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	zipBaseDir := r.File[0].Name

	// Extract each file
	for _, f := range r.File {
		// Skip the root directory in the archive
		if f.Name == "./" || f.Name == "." {
			continue
		}

		// Remove the root directory from the path
		path := strings.TrimPrefix(f.Name, "./")
		path = strings.TrimPrefix(path, ".")
		path = strings.TrimPrefix(path, zipBaseDir)
		target := filepath.Join(dst, path)

		// Create parent directories if they don't exist
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Skip directories (they're created by MkdirAll above)
		if f.FileInfo().IsDir() {
			continue
		}

		// Open the file in the zip
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in zip: %w", err)
		}

		// Create the file
		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create file: %w", err)
		}

		// Copy the file contents
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	return nil
}

// exists checks if a file or directory exists at the given path
func exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
