package acl

import (
	"io"
	"os"
	"path/filepath"

	yaml "github.com/goccy/go-yaml"
	"github.com/yashgorana/syftbox-go/pkg/utils"
)

// RuleSet represents a collection of access control rules that can be applied to a path.
// It can be marked as terminal to prevent further rule resolution up the directory tree.
type RuleSet struct {
	Terminal bool    `json:"terminal" yaml:"terminal"`
	Rules    []*Rule `json:"rules"    yaml:"rules"`

	// only for internal use
	path string
}

// NewRuleSet creates a new RuleSet instance with the given path, terminal flag, and initial rules.
func NewRuleSet(path string, terminal bool, rules ...*Rule) *RuleSet {
	return &RuleSet{
		path:     PathWithoutAclFileName(path),
		Terminal: terminal,
		Rules:    rules,
	}
}

// NewRuleSetFromPath creates a RuleSet by reading and parsing an ACL file from the specified path.
// The path should be the target path, not the ACL file path. The actual ACL file path will be
// computed using AsAclPath.
func NewRuleSetFromPath(path string) (*RuleSet, error) {
	aclPath := AsAclPath(path)
	fd, err := os.Open(aclPath)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return NewRuleSetFromReader(path, fd)
}

// NewRuleSetFromReader creates a RuleSet by reading and parsing YAML content from the provided reader.
// The path parameter is used to set the internal path of the RuleSet.
func NewRuleSetFromReader(path string, reader io.ReadCloser) (*RuleSet, error) {
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var perm RuleSet
	if err := yaml.Unmarshal(data, &perm); err != nil {
		return nil, err
	}

	perm.path = PathWithoutAclFileName(path)
	return setDefaults(&perm), nil
}

// Save the RuleSet as a YAML file at the specified path.
// It creates any necessary parent directories and sets appropriate file permissions.
func (p *RuleSet) Save(path string) error {
	if err := utils.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}

	bytes, err := yaml.MarshalWithOptions(p, yaml.Indent(2), yaml.UseSingleQuote(false))
	if err != nil {
		return err
	}

	return os.WriteFile(path, bytes, 0644)
}

func setDefaults(ruleset *RuleSet) *RuleSet {
	if len(ruleset.Rules) == 0 {
		ruleset.Rules = []*Rule{
			NewRule(Everyone, PrivateAccess(), DefaultLimits()),
		}
		return ruleset
	}

	for _, rule := range ruleset.Rules {
		if rule.Limits == nil {
			rule.Limits = DefaultLimits()
		}
		if rule.Access == nil {
			rule.Access = PrivateAccess()
		}
	}
	return ruleset
}
