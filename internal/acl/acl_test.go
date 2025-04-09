package acl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yashgorana/syftbox-go/internal/aclspec"
)

func TestAclServiceGetRule(t *testing.T) {
	service := NewAclService()

	// Add a ruleset
	ruleset := aclspec.NewRuleSet(
		"user",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)

	// Test cache miss rules
	assert.NotContains(t, service.cache.index, "user/readme.md")
	rule, err := service.GetRule("user/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)

	// test cache hit
	assert.Contains(t, service.cache.index, "user/readme.md")
	rule, err = service.GetRule("user/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)

	rule, err = service.GetRule("user/notes.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.txt", rule.rule.Pattern)
}

func TestAclServiceRemoveRuleSet(t *testing.T) {
	service := NewAclService()

	// Add two rulesets
	ruleset1 := aclspec.NewRuleSet(
		"folder1",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"folder2",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := service.AddRuleSet(ruleset1)
	assert.NoError(t, err)

	err = service.AddRuleSet(ruleset2)
	assert.NoError(t, err)

	// Verify both rulesets work
	rule, err := service.GetRule("folder1/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	rule, err = service.GetRule("folder2/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Remove one ruleset
	removed := service.RemoveRuleSet("folder1")
	assert.True(t, removed)

	// Verify removed ruleset no longer works
	rule, err = service.GetRule("folder1/file.txt")
	assert.Error(t, err)
	assert.Nil(t, rule)

	// Verify other ruleset still works
	rule, err = service.GetRule("folder2/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Try to remove non-existent ruleset
	removed = service.RemoveRuleSet("non-existent")
	assert.False(t, removed)
}

func TestAclServiceCanAccess(t *testing.T) {
	service := NewAclService()

	// Add a ruleset with different access levels
	ruleset := aclspec.NewRuleSet(
		"user",
		aclspec.SetTerminal,
		aclspec.NewRule("public/*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("private/*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("**", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)

	// Test cases with different users and files
	owner := &User{ID: "user", IsOwner: true}
	regularUser := &User{ID: aclspec.Everyone, IsOwner: false}

	publicFile := &File{Path: "user/public/doc.txt", Size: 100}
	privateFile := &File{Path: "user/private/secret.txt", Size: 100}
	aclFile := &File{Path: aclspec.AsAclPath("user"), Size: 100}

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
	assert.ErrorIs(t, err, ErrWriteRequired)

	err = service.CanAccess(regularUser, privateFile, AccessRead)
	assert.ErrorIs(t, err, ErrReadRequired)

	// ACL files should have special handling
	err = service.CanAccess(regularUser, aclFile, AccessWrite)
	assert.ErrorIs(t, err, ErrAdminRequired)
}

func TestAclServiceFileLimits(t *testing.T) {
	service := NewAclService()

	// Add a ruleset with file size limits
	limits := aclspec.Limits{
		MaxFileSize: 100,
		AllowDirs:   true,
	}

	ruleset := aclspec.NewRuleSet(
		"files",
		aclspec.SetTerminal,
		aclspec.NewRule("small/*.txt", aclspec.PublicReadWriteAccess(), &limits),
	)

	err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)

	regularUser := &User{IsOwner: false}

	// File within size limit
	smallFile := &File{Path: "files/small/small.txt", Size: 50}
	err = service.CanAccess(regularUser, smallFile, AccessWrite)
	assert.NoError(t, err)

	// File exceeding size limit
	largeFile := &File{Path: "files/small/large.txt", Size: 200}
	err = service.CanAccess(regularUser, largeFile, AccessWrite)
	assert.ErrorIs(t, err, ErrFileSizeExceeded)

	// Owner should bypass size limits
	owner := &User{IsOwner: true}
	err = service.CanAccess(owner, largeFile, AccessWrite)
	assert.NoError(t, err)
}

func TestAclServiceLoadRuleSets(t *testing.T) {
	service := NewAclService()

	// Create multiple rulesets
	ruleset1 := aclspec.NewRuleSet(
		"folder1",
		aclspec.SetTerminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"folder2",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	// Load multiple rulesets at once
	err := service.LoadRuleSets([]*aclspec.RuleSet{ruleset1, ruleset2})
	assert.NoError(t, err)

	// Verify both rulesets work
	rule, err := service.GetRule("folder1/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.txt", rule.rule.Pattern)

	rule, err = service.GetRule("folder2/file.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)
}

func TestAclServiceCacheInvalidation(t *testing.T) {
	service := NewAclService()

	// Add a ruleset
	rulesetv1 := aclspec.NewRuleSet(
		"user",
		aclspec.UnsetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	err := service.AddRuleSet(rulesetv1)
	assert.NoError(t, err)

	// Access a path to cache the rule
	rule, err := service.GetRule("user/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)
	assert.Contains(t, service.cache.index, "user/readme.md")

	// Replace the ruleset with different permissions
	rulesetv2 := aclspec.NewRuleSet(
		"user",
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	// Add new ruleset
	err = service.AddRuleSet(rulesetv2)
	assert.NoError(t, err)

	// Access the same path, should get the new rule
	rule, err = service.GetRule("user/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.True(t, rule.node.IsTerminal())
	assert.Equal(t, rule.node.Version(), uint8(2))
}
