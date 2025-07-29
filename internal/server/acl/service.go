package acl

import "github.com/openmined/syftbox/internal/aclspec"

type Service interface {
	AddRuleSet(ruleSet *aclspec.RuleSet) (ACLVersion, error)
	RemoveRuleSet(path string) bool
	GetRule(ctx *ACLRequest) (*ACLRule, error)
	CanAccess(ctx *ACLRequest) error
}
