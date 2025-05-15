package aclspec

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type PermissionType string

const (
	Read    PermissionType = "read"
	Create  PermissionType = "create"
	Write   PermissionType = "write"
	Execute PermissionType = "admin"
)

type LegacyRule struct {
	Path        string           `yaml:"path"`
	User        string           `yaml:"user"`
	Permissions []PermissionType `yaml:"permissions"`
}

type LegacyPermission struct {
	Rules []LegacyRule
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
// This allows LegacyPermission to be unmarshalled from a YAML sequence
// directly into its Rules field.
func (lp *LegacyPermission) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("cannot unmarshal %s into LegacyPermission.Rules, expected a sequence", node.Tag)
	}
	return node.Decode(&lp.Rules)
}
