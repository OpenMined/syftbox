package aclspec

// Rule represents a rule in the ACL file
// It contains a pattern, access control list, and optional limits
// The pattern is a glob string that specifies the files or directories to which the rule applies
// The access control list specifies the users who have access to the files or directories
// The limits specify the maximum number of files or directories that can be accessed
type Rule struct {
	Pattern string  `yaml:"pattern"`
	Access  *Access `yaml:"access"`
	Limits  *Limits `yaml:"-"` // todo - re-enable this later
}

// NewRule creates a new Rule with the specified pattern, access, and limits.
func NewRule(pattern string, access *Access, limits *Limits) *Rule {
	return &Rule{
		Pattern: pattern,
		Access:  access,
		Limits:  limits,
	}
}

// NewDefaultRule creates a new Rule with `**` as Pattern and the provided Access and Limits.
func NewDefaultRule(access *Access, limits *Limits) *Rule {
	return &Rule{
		Pattern: AllFiles,
		Access:  access,
		Limits:  limits,
	}
}
