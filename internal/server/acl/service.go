package acl

import "github.com/openmined/syftbox/internal/aclspec"

type Service interface {
	AddRuleSet(ruleSet *aclspec.RuleSet) (ACLVersion, error)
	RemoveRuleSet(path string) bool
	CanAccess(ctx *ACLRequest) error
}
