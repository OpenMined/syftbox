package datasite

import (
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openmined/syftbox/internal/utils"
	"gopkg.in/yaml.v3"
)

var (
	PathSep           = string(filepath.Separator)
	regexDatasitePath = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+/`)
)

// GetOwner returns the owner of the path
func GetOwner(path string) string {
	// clean path
	path = CleanPath(path)

	// get owner
	parts := strings.Split(path, PathSep)
	if len(parts) == 0 {
		return ""
	}

	return parts[0]
}

// IsOwner checks if the user is the owner of the path
// The underlying assumption here is that owner is the prefix of the path
func IsOwner(path string, user string) bool {
	path = CleanPath(path)
	return strings.HasPrefix(path, user)
}

// CleanPath returns a path with leading and trailing slashes removed
func CleanPath(path string) string {
	return strings.TrimLeft(filepath.Clean(path), PathSep)
}

// IsValidPath checks if the path is a valid datasite path
func IsValidPath(path string) bool {
	return regexDatasitePath.MatchString(path)
}

func IsValidDatasite(user string) bool {
	return utils.IsValidEmail(user)
}

// ExtractDatasiteName extracts the datasite name (email) from a path
// For example: "alice@example.com/public/file.txt" returns "alice@example.com"
func ExtractDatasiteName(path string) string {
	path = CleanPath(path)
	parts := strings.Split(path, PathSep)
	if len(parts) == 0 {
		return ""
	}
	
	// The first part should be the email
	email := parts[0]
	if IsValidDatasite(email) {
		return email
	}
	
	return ""
}

// SettingsYAML represents the structure of a settings.yaml file
type SettingsYAML struct {
	VanityDomains map[string]string `yaml:"domains"`
}

// ParseSettingsYAML parses a settings.yaml file from a reader
func ParseSettingsYAML(r io.Reader) (*SettingsYAML, error) {
	settings := &SettingsYAML{
		VanityDomains: make(map[string]string),
	}
	
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(settings); err != nil {
		// If error is EOF, return empty settings (valid empty file)
		if err == io.EOF {
			return settings, nil
		}
		return nil, err
	}
	
	// Initialize map if it's nil
	if settings.VanityDomains == nil {
		settings.VanityDomains = make(map[string]string)
	}
	
	return settings, nil
}
