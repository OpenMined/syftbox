package aclspec

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// RuleSet represents a set of rules for access control, including a list of rules and a terminal flag.
// Path is the location for which the rules apply.
type RuleSet struct {
	Rules    []*Rule `yaml:"rules,omitempty"`
	Terminal bool    `yaml:"terminal,omitempty"`
	Path     string  `yaml:"-"`
}

// NewRuleSet creates a new RuleSet instance with the given path, terminal flag, and initial rules.
func NewRuleSet(path string, terminal bool, rules ...*Rule) *RuleSet {
	return &RuleSet{
		Path:     WithoutAclPath(path),
		Terminal: terminal,
		Rules:    rules,
	}
}

func (r *RuleSet) AllRules() []*Rule {
	return r.Rules
}

// LoadFromFile loads a RuleSet from the specified file path
func LoadFromFile(path string) (*RuleSet, error) {
	aclPath := AsAclPath(path)
	fd, err := os.Open(aclPath)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return LoadFromReader(path, fd)
}

// LoadFromReader creates a RuleSet by reading and parsing YAML content from the provided reader.
// The path parameter is used to set the internal path of the RuleSet.
func LoadFromReader(path string, reader io.ReadCloser) (*RuleSet, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var ruleset RuleSet
	if err := yaml.Unmarshal(data, &ruleset); err != nil {
		return nil, err
	}

	ruleset.Path = WithoutAclPath(path)
	return setDefaults(&ruleset)
}

func (r *RuleSet) Save() error {
	aclPath := AsAclPath(r.Path)
	file, err := os.Create(aclPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", r.Path, err)
	}
	defer file.Close()

	// Create a new encoder with 2-space indentation
	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)

	// Encode the RuleSet to YAML
	if err := encoder.Encode(r); err != nil {
		return fmt.Errorf("failed to marshal RuleSet to YAML: %w", err)
	}

	return nil
}

func setDefaults(ruleset *RuleSet) (*RuleSet, error) {
	if ruleset.Rules == nil {
		ruleset.Rules = []*Rule{NewDefaultRule(PrivateAccess(), DefaultLimits())}
		return ruleset, nil
	}

	hasDefault := false
	for _, rule := range ruleset.Rules {
		if rule.Pattern == "" {
			return nil, fmt.Errorf("rule pattern cannot be empty")
		}

		if rule.Access == nil {
			return nil, fmt.Errorf("rule access cannot be nil")
		}

		if rule.Limits == nil {
			rule.Limits = DefaultLimits()
		}

		if rule.Pattern == AllFiles {
			hasDefault = true
		}
	}

	if !hasDefault {
		ruleset.Rules = append(ruleset.Rules, NewDefaultRule(PrivateAccess(), DefaultLimits()))
	}

	return ruleset, nil
}
