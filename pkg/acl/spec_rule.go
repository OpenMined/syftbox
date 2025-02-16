package acl

import "fmt"

type Rule struct {
	Pattern string  `json:"pattern" yaml:"pattern"`
	Access  *Access `json:"access"  yaml:"access"`
	Limits  *Limit  `json:"limits"  yaml:"limits"`
}

func NewRule(pattern string, access *Access, limit *Limit) *Rule {
	return &Rule{
		Pattern: pattern,
		Access:  access,
		Limits:  limit,
	}
}

func (r *Rule) String() string {
	return fmt.Sprintf("pattern:%s %s %s", r.Pattern, r.Access, r.Limits)
}
