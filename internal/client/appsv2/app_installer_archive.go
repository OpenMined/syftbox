package appsv2

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
	"resty.dev/v3"
)

func installFromArchive(ctx context.Context, parsedURL *url.URL, opts *AppInstallOpts, installDir string) error {
	// Get the archive URL
	archiveUrl, err := getArchiveUrl(parsedURL, opts)
	if err != nil {
		return fmt.Errorf("failed to get archive url: %w", err)
	}

	// Download the archive
	archivePath, err := downloadFile(ctx, archiveUrl)
	defer func() {
		if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove downloaded archive", "error", err)
		}
	}()
	if err != nil {
		return fmt.Errorf("failed to download archive: %w", err)
	}

	// Extract the archive
	if err := extractZip(archivePath, installDir); err != nil {
		// Attempt to clean up partially extracted files
		if rErr := os.RemoveAll(installDir); rErr != nil {
			slog.Warn("failed to cleanup partially extracted app directory", "path", installDir, "error", rErr)
		}

		return fmt.Errorf("failed to extract archive: %w", err)
	}

	return nil
}

// getArchiveUrl generates an archive URL based on the repository and options
func getArchiveUrl(repoUrl *url.URL, opts *AppInstallOpts) (string, error) {
	switch repoUrl.Host {
	case "github.com":
		return githubArchiveUrl(repoUrl.String(), opts.Branch, opts.Tag, opts.Commit)
	case "gitlab.com":
		return gitlabArchiveUrl(repoUrl.String(), opts.Branch, opts.Tag, opts.Commit)
	default:
		return "", fmt.Errorf("unsupported host: %q", repoUrl.Host)
	}
}

// githubArchiveUrl generates a GitHub archive URL based on the repository and options
func githubArchiveUrl(repoUrl, branch, tag, commit string) (string, error) {
	// github url scheme. supports zip, tar.gz
	// https://github.com/OpenMined/syft/archive/refs/heads/main.tar.gz
	// https://github.com/OpenMined/syft/archive/refs/tags/0.3.5.tar.gz
	// https://github.com/OpenMined/syft/archive/6eca36e8e46e64f557eb7ad344bd2a6be56d503e.tar.gz

	if !utils.IsValidURL(repoUrl) {
		return "", fmt.Errorf("invalid repository url: %q", repoUrl)
	}

	if branch != "" {
		return fmt.Sprintf("%s/archive/refs/heads/%s.zip", repoUrl, branch), nil
	}

	if tag != "" {
		return fmt.Sprintf("%s/archive/refs/tags/%s.zip", repoUrl, tag), nil
	}

	if commit != "" {
		return fmt.Sprintf("%s/archive/%s.zip", repoUrl, commit), nil
	}

	return "", fmt.Errorf("no branch, tag or commit provided")
}

// gitlabArchiveUrl generates a GitLab archive URL based on the repository and options
func gitlabArchiveUrl(repoUrl, branch, tag, commit string) (string, error) {
	// gitlab url scheme. supports zip, tar.gz
	// https://gitlab.com/gitlab-org/gitlab-runner/-/archive/main/archive.zip
	// https://gitlab.com/gitlab-org/gitlab-runner/-/archive/1dd26e1beea4eea6610ecd8cee97667ad6498145/archive.zip
	// https://gitlab.com/gitlab-org/gitlab-runner/-/archive/v17.10.1/archive.zip

	if !utils.IsValidURL(repoUrl) {
		return "", fmt.Errorf("invalid repository url: %q", repoUrl)
	}

	if branch != "" {
		return fmt.Sprintf("%s/-/archive/%s/archive.zip", repoUrl, branch), nil
	}

	if tag != "" {
		return fmt.Sprintf("%s/-/archive/%s/archive.zip", repoUrl, tag), nil
	}

	if commit != "" {
		return fmt.Sprintf("%s/-/archive/%s/archive.zip", repoUrl, commit), nil
	}

	return "", fmt.Errorf("no branch, tag or commit provided")
}

// downloadFile downloads a file from the given URL and returns the path to the downloaded file
func downloadFile(ctx context.Context, url string) (string, error) {
	client := resty.New()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "syftbox-app-*.zip")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Download the file
	resp, err := client.R().
		SetContext(ctx).
		SetOutputFileName(tmpFile.Name()).
		Get(url)
	if err != nil {
		return tmpFile.Name(), fmt.Errorf("http error: %w", err)
	}

	if !resp.IsSuccess() {
		return tmpFile.Name(), fmt.Errorf("status %s", resp.Status())
	}

	return tmpFile.Name(), nil
}

// extractZip extracts a .zip file to the target directory
func extractZip(zipPath, dst string) error {
	// Open the zip file
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("zip open %q: %w", zipPath, err)
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
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Skip directories (they're created by MkdirAll above)
		if f.FileInfo().IsDir() {
			continue
		}

		// Open the file in the zip
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("zip open file %q: %w", f.Name, err)
		}

		// Create the file
		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("zip extract file %q: %w", target, err)
		}

		// Copy the file contents
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return fmt.Errorf("zip extract file %q: %w", f.Name, err)
		}
	}

	return nil
}
