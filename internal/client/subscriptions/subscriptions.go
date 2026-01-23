package subscriptions

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const (
	FileName = "syft.sub.yaml"
)

type Action string

const (
	ActionAllow Action = "allow"
	ActionPause Action = "pause"
	ActionBlock Action = "block"
)

type Defaults struct {
	Action Action `yaml:"action" json:"action"`
}

type Rule struct {
	Action   Action `yaml:"action" json:"action"`
	Datasite string `yaml:"datasite,omitempty" json:"datasite,omitempty"`
	Path     string `yaml:"path" json:"path"`
}

type Config struct {
	Version  int      `yaml:"version" json:"version"`
	Defaults Defaults `yaml:"defaults" json:"defaults"`
	Rules    []Rule   `yaml:"rules" json:"rules"`
}

func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Defaults: Defaults{
			Action: ActionBlock,
		},
		Rules: []Rule{},
	}
}

func (a *Action) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		*a = ActionBlock
		return nil
	}
	action, err := parseAction(value.Value)
	if err != nil {
		return err
	}
	*a = action
	return nil
}

func (a *Action) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	action, err := parseAction(raw)
	if err != nil {
		return err
	}
	*a = action
	return nil
}

func parseAction(raw string) (Action, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ActionAllow):
		return ActionAllow, nil
	case string(ActionPause):
		return ActionPause, nil
	case string(ActionBlock), "deny":
		return ActionBlock, nil
	case "":
		return ActionBlock, nil
	default:
		return "", fmt.Errorf("invalid action %q", raw)
	}
}

func LoadAction(raw string) (Action, error) {
	return parseAction(raw)
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Defaults.Action == "" {
		cfg.Defaults.Action = ActionBlock
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Defaults.Action == "" {
		cfg.Defaults.Action = ActionBlock
	}

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func IsSubFile(p string) bool {
	p = normalizePath(p)
	return path.Base(p) == FileName
}

func (c *Config) ActionForPath(owner, relPath string) Action {
	if c == nil {
		return ActionBlock
	}

	relPath = normalizePath(relPath)
	if relPath == "" {
		return c.Defaults.Action
	}

	datasite, rest := splitDatasite(relPath)
	if datasite == "" {
		return c.Defaults.Action
	}
	if isOwnerDatasite(owner, datasite) {
		return ActionAllow
	}

	action := c.Defaults.Action
	fullPath := relPath

	for _, rule := range c.Rules {
		if !rule.matches(datasite, fullPath, rest) {
			continue
		}
		if rule.Action != "" {
			action = rule.Action
		}
	}

	return action
}

func (r Rule) matches(datasite, fullPath, pathWithin string) bool {
	if r.Path == "" {
		return false
	}
	if r.Datasite != "" {
		if !matchGlob(r.Datasite, datasite) {
			return false
		}
		return matchGlob(r.Path, pathWithin)
	}
	return matchGlob(r.Path, fullPath)
}

func matchGlob(pattern, target string) bool {
	pattern = normalizePath(pattern)
	target = normalizePath(target)
	ok, err := doublestar.Match(pattern, target)
	if err != nil {
		return false
	}
	return ok
}

func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimLeft(p, "/")
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	return p
}

func splitDatasite(relPath string) (string, string) {
	parts := strings.SplitN(relPath, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func isOwnerDatasite(owner, datasite string) bool {
	owner = strings.ToLower(strings.TrimSpace(owner))
	if owner == "" || datasite == "" {
		return false
	}
	return strings.EqualFold(owner, datasite)
}
