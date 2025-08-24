package acl

import (
	"slices"
	"sort"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
)

func calculateGlobSpecificity(glob string) int {
	// early return for the most specific glob patterns
	switch glob {
	case "**":
		return -100
	case "**/*":
		return -99
	}

	// 2L + 10D - wildcard penalty
	// Use forward slash for glob patterns
	score := len(glob)*2 + strings.Count(glob, ACLPathSep)*10

	if hasTemplatePattern(glob) {
		score += 50
	}

	// penalize base score for substr wildcards
	for i, c := range glob {
		switch c {
		case '*':
			if i == 0 {
				score -= 20 // Leading wildcards are very unspecific
			} else {
				score -= 10 // Other wildcards are less penalized
			}
		case '?', '!', '[', '{':
			score -= 2 // Non * wildcards get smaller penalty
		}
	}

	return score
}

func sortRulesBySpecificity(rules []*aclspec.Rule) []*aclspec.Rule {
	// copy the rules
	clone := slices.Clone(rules)

	// sort by specificity (or priority), descending
	sort.Slice(clone, func(i, j int) bool {
		return calculateGlobSpecificity(clone[i].Pattern) > calculateGlobSpecificity(clone[j].Pattern)
	})

	return clone
}

// GetOwner extracts the owner from the datasite path
func getOwner(path string) string {
	// get owner
	parts := strings.Split(path, ACLPathSep)
	if len(parts) == 0 {
		return ""
	}

	return parts[0]
}

// checks if the user is the owner of the path
func isOwner(path string, user string) bool {
	path = ACLNormPath(path)
	return strings.HasPrefix(path, user)
}
