package apps

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
)

func GetRunScript(app string) string {
	return filepath.Join(app, "run.sh")
}

// IsValidApp checks if a directory contains a valid app (has run.sh)
func IsValidApp(path string) bool {
	runScript := GetRunScript(path)
	return utils.FileExists(runScript)
}

func parseRepoURL(urlString string) (*url.URL, error) {
	parsedURL, err := url.ParseRequestURI(urlString)
	if err != nil {
		return nil, fmt.Errorf("invalid url %q: %w", urlString, err)
	}
	parsedURL.Path = strings.TrimSuffix(parsedURL.Path, ".git")
	return parsedURL, nil
}

// returns a reverse domain name from a url
func appIDFromURL(url *url.URL) string {
	if url.Host == "" || url.Path == "" {
		return ""
	}

	// Split the host into parts and reverse them
	parts := strings.Split(url.Host, ".")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}

	// Get the path parts and remove empty strings
	pathParts := strings.Split(strings.Trim(url.Path, "/"), "/")
	var filteredPathParts []string
	for _, part := range pathParts {
		if part != "" {
			// Replace any dots with hyphens in path parts
			part = strings.ReplaceAll(part, ".", "-")
			filteredPathParts = append(filteredPathParts, strings.ToLower(part))
		}
	}

	// Combine all parts with dots
	allParts := append(parts, filteredPathParts...)
	return strings.Join(allParts, ".")
}

// returns a reverse domain name from a local path
func appIDFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.ReplaceAll(base, ".", "-")
	return fmt.Sprintf("local.%s", base)
}

// returns basename of a url as the app name
func appNameFromURL(repoUrl *url.URL) string {
	return appNameFromPath(repoUrl.Path)
}

// returns the basename of a path as the app name
func appNameFromPath(path string) string {
	base := filepath.Base(path)
	return strings.ToLower(base)
}
