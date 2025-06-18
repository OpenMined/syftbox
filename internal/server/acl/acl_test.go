package acl

import (
	"testing"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/stretchr/testify/assert"
)

func TestAclServiceGetRule(t *testing.T) {
	service := NewACLService()

	// Add a ruleset
	ruleset := aclspec.NewRuleSet(
		"user",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test cache miss rules
	assert.NotContains(t, service.cache.index, "user/readme.md")
	rule, err := service.GetEffectiveRule("user/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)

	// test cache hit
	assert.Contains(t, service.cache.index, "user/readme.md")
	rule, err = service.GetEffectiveRule("user/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)

	rule, err = service.GetEffectiveRule("user/notes.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.txt", rule.rule.Pattern)
}

func TestAclServiceRemoveRuleSet(t *testing.T) {
	service := NewACLService()

	// Add two rulesets
	ruleset1 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"user2@email.com",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset1)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	ver, err = service.AddRuleSet(ruleset2)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Verify both rulesets work
	rule, err := service.GetEffectiveRule("user1@email.com/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	rule, err = service.GetEffectiveRule("user2@email.com/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Remove one ruleset
	removed := service.RemoveRuleSet("user1@email.com")
	assert.True(t, removed)

	// Verify removed ruleset no longer works
	rule, err = service.GetEffectiveRule("user1@email.com/file.txt")
	assert.Error(t, err)
	assert.Nil(t, rule)

	// Verify other ruleset still works
	rule, err = service.GetEffectiveRule("user2@email.com/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Try to remove non-existent ruleset
	removed = service.RemoveRuleSet("non-existent")
	assert.False(t, removed)
}

func TestAclServiceCanAccess(t *testing.T) {
	service := NewACLService()

	// Add a ruleset with different access levels
	ruleset := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.SetTerminal,
		aclspec.NewRule("public/*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("private/*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("**", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test cases with different users and files
	owner := &User{ID: "user1@email.com"}
	regularUser := &User{ID: aclspec.Everyone}

	publicFile := &File{Path: "user1@email.com/public/doc.txt", Size: 100}
	privateFile := &File{Path: "user1@email.com/private/secret.txt", Size: 100}
	aclFile := &File{Path: aclspec.AsACLPath("user1@email.com"), Size: 100}

	// Owner should have access to everything
	err = service.CanAccess(owner, publicFile, AccessRead)
	assert.NoError(t, err)

	err = service.CanAccess(owner, privateFile, AccessWrite)
	assert.NoError(t, err)

	err = service.CanAccess(owner, aclFile, AccessRead)
	assert.NoError(t, err)

	// Regular user should have limited access
	err = service.CanAccess(regularUser, publicFile, AccessRead)
	assert.NoError(t, err)

	err = service.CanAccess(regularUser, publicFile, AccessWrite)
	assert.ErrorIs(t, err, ErrNoWriteAccess)

	err = service.CanAccess(regularUser, privateFile, AccessRead)
	assert.ErrorIs(t, err, ErrNoReadAccess)

	// ACL files should have special handling
	err = service.CanAccess(regularUser, aclFile, AccessWrite)
	assert.ErrorIs(t, err, ErrNoAdminAccess)
}

func TestAclServiceFileLimits(t *testing.T) {
	service := NewACLService()

	owner := "user1@email.com"
	someUser := "user2@email.com"

	ruleset := aclspec.NewRuleSet(
		owner,
		aclspec.SetTerminal,
		aclspec.NewRule(
			"dir/*.txt",
			aclspec.PublicReadWriteAccess(),
			&aclspec.Limits{MaxFileSize: 100, AllowDirs: true},
		),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// File within size limit
	smallFile := &File{Path: "user1@email.com/dir/small.txt", Size: 50}
	err = service.CanAccess(&User{ID: someUser}, smallFile, AccessWrite)
	assert.NoError(t, err)

	// File exceeding size limit
	largeFile := &File{Path: "user1@email.com/dir/large.txt", Size: 200}
	err = service.CanAccess(&User{ID: someUser}, largeFile, AccessWrite)
	assert.ErrorIs(t, err, ErrFileSizeExceeded)

	// Owner should bypass size limits
	err = service.CanAccess(&User{ID: owner}, largeFile, AccessWrite)
	assert.NoError(t, err)
}

func TestAclServiceLoadRuleSets(t *testing.T) {
	service := NewACLService()

	// Create multiple rulesets
	ruleset1 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"user2@email.com",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	// Load multiple rulesets at once
	err := service.AddRuleSets([]*aclspec.RuleSet{ruleset1, ruleset2})
	assert.NoError(t, err)

	// Verify both rulesets work
	rule, err := service.GetEffectiveRule("user1@email.com/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.txt", rule.rule.Pattern)

	rule, err = service.GetEffectiveRule("user2@email.com/file.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)
}

func TestAclServiceCacheInvalidation(t *testing.T) {
	service := NewACLService()

	// Add a ruleset
	rulesetv1 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(rulesetv1)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Access a path to cache the rule
	rule, err := service.GetEffectiveRule("user1@email.com/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)
	assert.Contains(t, service.cache.index, "user1@email.com/readme.md")

	// Replace the ruleset with different permissions
	rulesetv2 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	// Add new ruleset
	ver, err = service.AddRuleSet(rulesetv2)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(2), ver)

	// Access the same path, should get the new rule
	rule, err = service.GetEffectiveRule("user1@email.com/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.True(t, rule.node.GetTerminal())
	assert.Equal(t, rule.node.GetVersion(), ACLVersion(2))
}
