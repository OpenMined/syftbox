package acl

import mapset "github.com/deckarep/golang-set/v2"

var empty = []string{}

func NewLimits(maxFiles uint32, maxFileSize uint64, allowDirs, allowSymlinks bool) *Limit {
	return &Limit{
		MaxFiles:      maxFiles,
		MaxFileSize:   maxFileSize,
		AllowDirs:     allowDirs,
		AllowSymlinks: allowSymlinks,
	}
}

func DefaultLimits() *Limit {
	return &Limit{
		MaxFiles:      0,
		MaxFileSize:   0,
		AllowDirs:     true,
		AllowSymlinks: false,
	}
}

func NewRuleSet(terminal bool, rules ...*Rule) *RuleSet {
	return &RuleSet{
		Rules:    rules,
		Terminal: terminal,
	}
}

func NewRule(pattern string, access *Access, limit *Limit) *Rule {
	return &Rule{
		Pattern: pattern,
		Access:  access,
		Limits:  limit,
	}
}

func NewAccess(admin []string, write []string, read []string) *Access {
	return &Access{
		Admin: mapset.NewSet(admin...),
		Write: mapset.NewSet(write...),
		Read:  mapset.NewSet(read...),
	}
}

func PrivateAccess() *Access {
	return NewAccess(empty, empty, empty)
}

func PublicReadAccess() *Access {
	return NewAccess(empty, empty, []string{Everyone})
}

func PublicReadWriteAccess() *Access {
	return NewAccess(empty, []string{Everyone}, empty)
}

func SharedReadAccess(users ...string) *Access {
	return NewAccess(empty, empty, users)
}

func SharedReadWriteAccess(users ...string) *Access {
	return NewAccess(empty, users, empty)
}
