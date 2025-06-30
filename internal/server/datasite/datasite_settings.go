package datasite

import (
	"io"

	"gopkg.in/yaml.v3"
)

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
