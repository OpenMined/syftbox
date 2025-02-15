package acl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
	mapset "github.com/deckarep/golang-set/v2"
	yaml "github.com/goccy/go-yaml"

	"github.com/yashgorana/syftbox-go/pkg/utils"
)

const (
	Everyone    = "*"
	AclFileName = "syft.pub.yaml"
	AllFiles    = "**"
)

type Access struct {
	Admin mapset.Set[string] `json:"admin" yaml:"admin"`
	Read  mapset.Set[string] `json:"read"  yaml:"read"`
	Write mapset.Set[string] `json:"write" yaml:"write"`
}

func (a *Access) String() string {
	return fmt.Sprintf("r:%s w:%s a:%s", a.Read.String(), a.Write.String(), a.Admin.String())
}

type Limit struct {
	MaxFiles      uint32 `json:"maxFiles"      yaml:"maxFiles"`
	MaxFileSize   uint64 `json:"maxFileSize"   yaml:"maxFileSize"`
	AllowDirs     bool   `json:"allowDirs"     yaml:"allowDirs"`
	AllowSymlinks bool   `json:"allowSymlinks" yaml:"allowSymlinks"`
}

type Rule struct {
	Pattern string  `json:"glob"   yaml:"glob,path"`
	Access  *Access `json:"access" yaml:"access"`
	Limits  *Limit  `json:"limit"  yaml:"limits"`
}

type RuleSet struct {
	Terminal bool    `json:"terminal" yaml:"terminal"`
	Rules    []*Rule `json:"rules"    yaml:"rules"`
}

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

// Load reads permissions from disk
func Load(path string) (*RuleSet, error) {
	aclPath := resolvePath(path)
	data, err := os.ReadFile(aclPath)
	if err != nil {
		return nil, err
	}

	var perm RuleSet
	if err := yaml.Unmarshal(data, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

func (p *RuleSet) SaveJSON(path string) error {
	if err := utils.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}

	bytes, err := sonic.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0644)
}

func (p *RuleSet) LoadJSON(path string) (*RuleSet, error) {
	aclpath := resolvePath(path)
	data, err := os.ReadFile(aclpath)
	if err != nil {
		return nil, err
	}

	var perm RuleSet
	if err := sonic.Unmarshal(data, &perm); err != nil {
		return nil, err
	}

	return &perm, nil
}

func (p *RuleSet) AddRule(rule *Rule) {
	p.Rules = append(p.Rules, rule)
}

func (a Access) MarshalYAML() (interface{}, error) {
	m := make(map[string][]string)
	if a.Admin != nil {
		m["admin"] = a.Admin.ToSlice()
	}
	if a.Read != nil {
		m["read"] = a.Read.ToSlice()
	}
	if a.Write != nil {
		m["write"] = a.Write.ToSlice()
	}
	return m, nil
}

func (a *Access) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var m map[string][]string
	if err := unmarshal(&m); err != nil {
		return err
	}

	if admin, ok := m["admin"]; ok {
		a.Admin = mapset.NewSet(admin...)
	}
	if read, ok := m["read"]; ok {
		a.Read = mapset.NewSet(read...)
	}
	if write, ok := m["write"]; ok {
		a.Write = mapset.NewSet(write...)
	}

	return nil
}

// resolvePath resolves a path to a permissions file path
func resolvePath(path string) string {
	if isAclFile(path) {
		return filepath.Join(filepath.Dir(path), AclFileName)
	}
	return filepath.Join(path, AclFileName)
}

func isAclFile(path string) bool {
	return strings.HasSuffix(path, AclFileName)
}
