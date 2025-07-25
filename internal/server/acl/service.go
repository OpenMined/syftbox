package acl

import "github.com/openmined/syftbox/internal/aclspec"

type Service interface {
	AddRuleSet(ruleSet *aclspec.RuleSet) (ACLVersion, error)
	RemoveRuleSet(path string) bool
	GetRule(path string) (*ACLRule, error)
	CanAccess(user *User, file *File, level AccessLevel) error
}
