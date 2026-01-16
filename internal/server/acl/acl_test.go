package acl

import (
	"context"
	"iter"
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockBlobService is a mock implementation of blob.Service
type MockBlobService struct {
	mock.Mock
}

func (m *MockBlobService) Backend() blob.IBlobBackend {
	args := m.Called()
	return args.Get(0).(blob.IBlobBackend)
}

func (m *MockBlobService) Index() blob.IBlobIndex {
	args := m.Called()
	return args.Get(0).(blob.IBlobIndex)
}

func (m *MockBlobService) OnBlobChange(callback blob.BlobChangeCallback) {
	m.Called(callback)
}

// MockBlobIndex is a mock implementation of blob.IBlobIndex
type MockBlobIndex struct {
	mock.Mock
}

func (m *MockBlobIndex) Get(key string) (*blob.BlobInfo, bool) {
	args := m.Called(key)
	return args.Get(0).(*blob.BlobInfo), args.Bool(1)
}

func (m *MockBlobIndex) Set(blob *blob.BlobInfo) error {
	args := m.Called(blob)
	return args.Error(0)
}

func (m *MockBlobIndex) SetMany(blobs []*blob.BlobInfo) error {
	args := m.Called(blobs)
	return args.Error(0)
}

func (m *MockBlobIndex) Remove(key string) error {
	args := m.Called(key)
	return args.Error(0)
}

func (m *MockBlobIndex) List() ([]*blob.BlobInfo, error) {
	args := m.Called()
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) Iter() iter.Seq[*blob.BlobInfo] {
	args := m.Called()
	return args.Get(0).(iter.Seq[*blob.BlobInfo])
}

func (m *MockBlobIndex) Count() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockBlobIndex) FilterByKeyGlob(pattern string) ([]*blob.BlobInfo, error) {
	args := m.Called(pattern)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterByPrefix(prefix string) ([]*blob.BlobInfo, error) {
	args := m.Called(prefix)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterBySuffix(suffix string) ([]*blob.BlobInfo, error) {
	args := m.Called(suffix)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterByTime(filter blob.TimeFilter) ([]*blob.BlobInfo, error) {
	args := m.Called(filter)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterAfterTime(after time.Time) ([]*blob.BlobInfo, error) {
	args := m.Called(after)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobIndex) FilterBeforeTime(before time.Time) ([]*blob.BlobInfo, error) {
	args := m.Called(before)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

// MockBlobBackend is a mock implementation of blob.IBlobBackend
type MockBlobBackend struct {
	mock.Mock
}

func (m *MockBlobBackend) GetObject(ctx context.Context, key string) (*blob.GetObjectResponse, error) {
	args := m.Called(ctx, key)
	return args.Get(0).(*blob.GetObjectResponse), args.Error(1)
}

func (m *MockBlobBackend) GetObjectPresigned(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockBlobBackend) PutObject(ctx context.Context, params *blob.PutObjectParams) (*blob.PutObjectResponse, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*blob.PutObjectResponse), args.Error(1)
}

func (m *MockBlobBackend) PutObjectPresigned(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockBlobBackend) PutObjectMultipart(ctx context.Context, params *blob.PutObjectMultipartParams) (*blob.PutObjectMultipartResponse, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*blob.PutObjectMultipartResponse), args.Error(1)
}

func (m *MockBlobBackend) CompleteMultipartUpload(ctx context.Context, params *blob.CompleteMultipartUploadParams) (*blob.PutObjectResponse, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*blob.PutObjectResponse), args.Error(1)
}

func (m *MockBlobBackend) CopyObject(ctx context.Context, params *blob.CopyObjectParams) (*blob.CopyObjectResponse, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*blob.CopyObjectResponse), args.Error(1)
}

func (m *MockBlobBackend) DeleteObject(ctx context.Context, key string) (bool, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Error(1)
}

func (m *MockBlobBackend) ListObjects(ctx context.Context) ([]*blob.BlobInfo, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*blob.BlobInfo), args.Error(1)
}

func (m *MockBlobBackend) Delegate() any {
	args := m.Called()
	return args.Get(0)
}

// Note: setHooks is a private method not needed for testing

func aclSvc() *ACLService {
	// Create mock dependencies
	mockIndex := &MockBlobIndex{}
	mockBackend := &MockBlobBackend{}
	mockBlobService := &MockBlobService{}

	// Set up common expectations
	mockBlobService.On("Index").Return(mockIndex)
	mockBlobService.On("Backend").Return(mockBackend)
	mockBlobService.On("OnBlobChange", mock.AnythingOfType("blob.BlobChangeCallback")).Return()

	// Set up index expectations with sensible defaults
	mockIndex.On("FilterBySuffix", mock.AnythingOfType("string")).Return([]*blob.BlobInfo{}, nil)
	mockIndex.On("Iter").Return(func(yield func(*blob.BlobInfo) bool) {
		// Empty iterator by default
	})
	mockIndex.On("Count").Return(0)

	return NewACLService(mockBlobService)
}

func TestAclServiceGetRule(t *testing.T) {
	service := aclSvc()

	// Add a ruleset
	ruleset := aclspec.NewRuleSet(
		"user",
		aclspec.Terminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test getting compiled rules through the tree
	req := NewRequest("user/readme.md", &User{ID: "testuser"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)

	// Test another file pattern
	req2 := NewRequest("user/notes.txt", &User{ID: "testuser"}, AccessRead)
	rule2, err := service.tree.GetCompiledRule(req2)
	assert.NoError(t, err)
	assert.NotNil(t, rule2)
	assert.Equal(t, "*.txt", rule2.rule.Pattern)
}

func TestAclServiceRemoveRuleSet(t *testing.T) {
	service := aclSvc()

	// Add two rulesets
	ruleset1 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.Terminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"user2@email.com",
		aclspec.Terminal,
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset1)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	ver, err = service.AddRuleSet(ruleset2)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Verify both rulesets work
	req1 := NewRequest("user1@email.com/file.txt", &User{ID: "testuser"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req1)
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	req2 := NewRequest("user2@email.com/file.txt", &User{ID: "testuser"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req2)
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Remove one ruleset
	removed := service.RemoveRuleSet("user1@email.com")
	assert.True(t, removed)

	// Verify removed ruleset no longer works
	req1 = NewRequest("user1@email.com/file.txt", &User{ID: "testuser"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req1)
	assert.Error(t, err)
	assert.Nil(t, rule)

	// Verify other ruleset still works
	req2 = NewRequest("user2@email.com/file.txt", &User{ID: "testuser"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req2)
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Try to remove non-existent ruleset
	removed = service.RemoveRuleSet("non-existent")
	assert.False(t, removed)
}

func TestAclServiceCanAccess(t *testing.T) {
	service := aclSvc()

	// Add a ruleset with different access levels
	ruleset := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.Terminal,
		aclspec.NewRule("public/*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("private/*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("**", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test cases with different users and files
	owner := &User{ID: "user1@email.com"}
	regularUser := &User{ID: aclspec.TokenEveryone}

	publicFile := &File{Size: 100, IsDir: false, IsSymlink: false}
	privateFile := &File{Size: 100, IsDir: false, IsSymlink: false}
	aclFile := &File{Size: 100, IsDir: false, IsSymlink: false}

	// Owner should have access to everything
	req := NewRequestWithFile("user1@email.com/public/doc.txt", owner, AccessRead, publicFile)
	err = service.CanAccess(req)
	assert.NoError(t, err)

	req = NewRequestWithFile("user1@email.com/private/secret.txt", owner, AccessWrite, privateFile)
	err = service.CanAccess(req)
	assert.NoError(t, err)

	req = NewRequestWithFile(aclspec.AsACLPath("user1@email.com"), owner, AccessRead, aclFile)
	err = service.CanAccess(req)
	assert.NoError(t, err)

	// Regular user should have limited access
	req = NewRequestWithFile("user1@email.com/public/doc.txt", regularUser, AccessRead, publicFile)
	err = service.CanAccess(req)
	assert.NoError(t, err)

	req = NewRequestWithFile("user1@email.com/public/doc.txt", regularUser, AccessWrite, publicFile)
	err = service.CanAccess(req)
	assert.ErrorIs(t, err, ErrNoWriteAccess)

	req = NewRequestWithFile("user1@email.com/private/secret.txt", regularUser, AccessRead, privateFile)
	err = service.CanAccess(req)
	assert.ErrorIs(t, err, ErrNoReadAccess)

	// ACL files should have special handling
	req = NewRequestWithFile(aclspec.AsACLPath("user1@email.com"), regularUser, AccessRead, aclFile)
	err = service.CanAccess(req)
	assert.NoError(t, err)

	req = NewRequestWithFile(aclspec.AsACLPath("user1@email.com"), regularUser, AccessWrite, aclFile)
	err = service.CanAccess(req)
	assert.ErrorIs(t, err, ErrNoAdminAccess)
}

func TestAclServiceFileLimits(t *testing.T) {
	service := aclSvc()

	owner := "user1@email.com"
	someUser := "user2@email.com"

	ruleset := aclspec.NewRuleSet(
		owner,
		aclspec.Terminal,
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
	smallFile := &File{Size: 50, IsDir: false, IsSymlink: false}
	req := NewRequestWithFile("user1@email.com/dir/small.txt", &User{ID: someUser}, AccessWrite, smallFile)
	err = service.CanAccess(req)
	assert.NoError(t, err)

	// File exceeding size limit
	largeFile := &File{Size: 200, IsDir: false, IsSymlink: false}
	req = NewRequestWithFile("user1@email.com/dir/large.txt", &User{ID: someUser}, AccessWrite, largeFile)
	err = service.CanAccess(req)
	assert.ErrorIs(t, err, ErrFileSizeExceeded)

	// Owner should bypass size limits
	req = NewRequestWithFile("user1@email.com/dir/large.txt", &User{ID: owner}, AccessWrite, largeFile)
	err = service.CanAccess(req)
	assert.NoError(t, err)
}

func TestAclServiceLoadRuleSets(t *testing.T) {
	service := aclSvc()

	// Create multiple rulesets
	ruleset1 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.Terminal,
		aclspec.NewRule("*.txt", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ruleset2 := aclspec.NewRuleSet(
		"user2@email.com",
		aclspec.Terminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	// Load multiple rulesets at once
	ver, err := service.AddRuleSet(ruleset1)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	ver, err = service.AddRuleSet(ruleset2)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)
	assert.NoError(t, err)

	// Verify both rulesets work
	req1 := NewRequest("user1@email.com/file.txt", &User{ID: "testuser"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req1)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.txt", rule.rule.Pattern)

	req2 := NewRequest("user2@email.com/file.md", &User{ID: "testuser"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req2)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)
}

func TestAclServiceCacheInvalidation(t *testing.T) {
	service := aclSvc()

	// Add a ruleset
	rulesetv1 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.NotTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(rulesetv1)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Access a path to get a compiled rule
	req := NewRequest("user1@email.com/readme.md", &User{ID: "testuser"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)

	// Replace the ruleset with different permissions
	rulesetv2 := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.Terminal,
		aclspec.NewRule("*.md", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	// Add new ruleset
	ver, err = service.AddRuleSet(rulesetv2)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(2), ver)

	// Access the same path, should get the new rule
	req = NewRequest("user1@email.com/readme.md", &User{ID: "testuser"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.True(t, rule.node.GetTerminal())
	assert.Equal(t, rule.node.GetVersion(), ACLVersion(2))
}

func TestAclServiceTemplatePatterns(t *testing.T) {
	service := aclSvc()

	// Test template with UserEmail variable
	ruleset := aclspec.NewRuleSet(
		"user1@email.com",
		aclspec.Terminal,
		aclspec.NewRule("private_{{.UserEmail}}/*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("public/*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("user_{{.UserEmail}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("hash_{{.UserHash}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("year_{{.Year}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("month_{{.Month}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("day_{{.Date}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test that template resolves correctly for the owner
	req := NewRequest("user1@email.com/private_user1@email.com/document.txt", &User{ID: "user1@email.com"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "private_{{.UserEmail}}/*.txt", rule.rule.Pattern)

	// Test that template doesn't match for different user
	req = NewRequest("user1@email.com/private_user2@email.com/document.txt", &User{ID: "user1@email.com"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req)
	// Should match the public rule instead
	if err == nil {
		assert.Equal(t, "public/*.md", rule.rule.Pattern) // Should fall back to other patterns
	}
}

func TestAclServiceTemplateVariables(t *testing.T) {
	service := aclSvc()

	// Test various template variables
	ruleset := aclspec.NewRuleSet(
		"templates@example.com",
		aclspec.Terminal,
		aclspec.NewRule("user_{{.UserEmail}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("hash_{{.UserHash}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("year_{{.Year}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("month_{{.Month}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("day_{{.Date}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test UserEmail template
	req := NewRequest("templates@example.com/user_test@example.com/file.txt", &User{ID: "test@example.com"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "user_{{.UserEmail}}/*", rule.rule.Pattern)

	// Verify UserHash template rule exists (can't easily test matching due to hash unpredictability)
	rules := service.tree.GetNearestNode("templates@example.com").GetRules()
	found := false
	for _, r := range rules {
		if r.rule.Pattern == "hash_{{.UserHash}}/*" {
			found = true
			break
		}
	}
	assert.True(t, found, "UserHash template rule should exist")
}

func TestAclServiceTemplateFunctions(t *testing.T) {
	service := aclSvc()

	// Test template functions
	ruleset := aclspec.NewRuleSet(
		"funcs@example.com",
		aclspec.Terminal,
		aclspec.NewRule("upper_{{upper .UserEmail}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("lower_{{lower .UserEmail}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("hash_{{sha2 .UserEmail}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("short_hash_{{sha2 .UserEmail 8}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test upper function
	req := NewRequest("funcs@example.com/upper_TEST@EXAMPLE.COM/file.txt", &User{ID: "test@example.com"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "upper_{{upper .UserEmail}}/*", rule.rule.Pattern)

	// Test lower function
	req = NewRequest("funcs@example.com/lower_test@example.com/file.txt", &User{ID: "Test@Example.Com"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "lower_{{lower .UserEmail}}/*", rule.rule.Pattern)

	// Verify all template function rules exist
	rules := service.tree.GetNearestNode("funcs@example.com").GetRules()
	patterns := make([]string, len(rules))
	for i, r := range rules {
		patterns[i] = r.rule.Pattern
	}
	assert.Contains(t, patterns, "upper_{{upper .UserEmail}}/*")
	assert.Contains(t, patterns, "lower_{{lower .UserEmail}}/*")
	assert.Contains(t, patterns, "hash_{{sha2 .UserEmail}}/*")
	assert.Contains(t, patterns, "short_hash_{{sha2 .UserEmail 8}}/*")
}

func TestAclServiceTemplateWithGlobPatterns(t *testing.T) {
	service := aclSvc()

	// Test templates combined with glob patterns
	ruleset := aclspec.NewRuleSet(
		"mixed@example.com",
		aclspec.Terminal,
		aclspec.NewRule("{{.UserEmail}}/private/**/*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("{{.UserEmail}}/public/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("shared/{{.Year}}/*.log", aclspec.SharedReadAccess("admin@example.com"), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test deep glob pattern with template
	req := NewRequest("mixed@example.com/user@test.com/private/deep/nested/file.txt", &User{ID: "user@test.com"}, AccessRead)
	rule, err := service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "{{.UserEmail}}/private/**/*.txt", rule.rule.Pattern)

	// Test simple glob pattern with template
	req = NewRequest("mixed@example.com/user@test.com/public/readme.md", &User{ID: "user@test.com"}, AccessRead)
	rule, err = service.tree.GetCompiledRule(req)
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "{{.UserEmail}}/public/*", rule.rule.Pattern)
}

func TestAclServiceTemplateAccessControl(t *testing.T) {
	service := aclSvc()

	// Test access control with templates - templates in patterns only
	ruleset := aclspec.NewRuleSet(
		"access@example.com",
		aclspec.Terminal,
		aclspec.NewRule("private_{{.UserEmail}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("public/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Owner should have access to everything
	req := NewRequestWithFile("access@example.com/private_user1@example.com/document.txt", &User{ID: "access@example.com"}, AccessWrite, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "Owner should have access")

	// Test that template pattern matching works - user accessing path that matches their email template
	req = NewRequestWithFile("access@example.com/private_user1@example.com/document.txt", &User{ID: "user1@example.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "User should have access when template resolves to their path")

	// Test that template pattern doesn't match when user email doesn't match path
	req = NewRequestWithFile("access@example.com/private_user2@example.com/document.txt", &User{ID: "user1@example.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	rule, err := service.tree.GetCompiledRule(req)
	// This should either find no rule or find the public rule as fallback
	if err == nil && rule != nil {
		// If a rule is found, it should be the public rule, not the template rule
		assert.Equal(t, "public/*", rule.rule.Pattern, "Should match public rule when template doesn't match")
	}

	// Test that anyone can access public files
	req = NewRequestWithFile("access@example.com/public/document.txt", &User{ID: "anyone@example.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "Anyone should have read access to public files")

	// Add tests for access control with glob email patterns
	globRuleset := aclspec.NewRuleSet(
		"glob@example.com",
		aclspec.Terminal,
		// Read access for all users from example.com domain
		aclspec.NewRule("domain_read/*", aclspec.SharedReadAccess("*@example.com"), aclspec.DefaultLimits()),
		// Write access for users starting with "admin" from any domain
		aclspec.NewRule("admin_write/*", aclspec.SharedReadWriteAccess("admin*"), aclspec.DefaultLimits()),
		// Admin access for specific admin pattern
		aclspec.NewRule("admin_files/*", aclspec.NewAccess([]string{"admin*@example.com"}, []string{}, []string{}), aclspec.DefaultLimits()),
		// Multiple glob patterns for read access
		aclspec.NewRule("multi_glob/*", aclspec.SharedReadAccess("user*@test.com", "*@example.org"), aclspec.DefaultLimits()),
	)

	ver, err = service.AddRuleSet(globRuleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Test domain glob pattern for read access
	req = NewRequestWithFile("glob@example.com/domain_read/file.txt", &User{ID: "user1@example.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "User from example.com domain should have read access")

	req = NewRequestWithFile("glob@example.com/domain_read/file.txt", &User{ID: "user2@example.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "Another user from example.com domain should have read access")

	req = NewRequestWithFile("glob@example.com/domain_read/file.txt", &User{ID: "user@otherdomain.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.Error(t, err, "User from different domain should not have access")

	// Test admin prefix glob pattern for write access
	req = NewRequestWithFile("glob@example.com/admin_write/file.txt", &User{ID: "admin1@example.com"}, AccessWrite, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "Admin user should have write access")

	req = NewRequestWithFile("glob@example.com/admin_write/file.txt", &User{ID: "administrator@test.com"}, AccessWrite, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "User starting with 'admin' should have write access")

	req = NewRequestWithFile("glob@example.com/admin_write/file.txt", &User{ID: "user@example.com"}, AccessWrite, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.Error(t, err, "Regular user should not have write access")

	// Test admin access with domain-specific glob pattern
	req = NewRequestWithFile("glob@example.com/admin_files/file.txt", &User{ID: "admin1@example.com"}, AccessAdmin, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "Admin from example.com should have admin access")

	req = NewRequestWithFile("glob@example.com/admin_files/file.txt", &User{ID: "admin@otherdomain.com"}, AccessAdmin, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.Error(t, err, "Admin from different domain should not have admin access")

	// Test multiple glob patterns
	req = NewRequestWithFile("glob@example.com/multi_glob/file.txt", &User{ID: "user123@test.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "User matching first glob pattern should have read access")

	req = NewRequestWithFile("glob@example.com/multi_glob/file.txt", &User{ID: "anyone@example.org"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.NoError(t, err, "User matching second glob pattern should have read access")

	req = NewRequestWithFile("glob@example.com/multi_glob/file.txt", &User{ID: "nomatch@random.com"}, AccessRead, &File{Size: 100, IsDir: false, IsSymlink: false})
	err = service.CanAccess(req)
	assert.Error(t, err, "User not matching any glob pattern should not have access")
}

func TestAclServiceComplexTemplatePatterns(t *testing.T) {
	service := aclSvc()

	// Test complex template patterns with multiple variables and functions
	ruleset := aclspec.NewRuleSet(
		"complex@example.com",
		aclspec.Terminal,
		aclspec.NewRule("{{.Year}}/{{.Month}}/{{lower .UserEmail}}/*", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("archive/{{.Year}}-{{.Month}}-{{.Date}}/*", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("users/{{sha2 .UserEmail 16}}/{{upper .UserEmail}}/*", aclspec.SharedReadAccess("admin@example.com"), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	// Verify all complex template rules were created
	rules := service.tree.GetNearestNode("complex@example.com").GetRules()
	patterns := make([]string, len(rules))
	for i, r := range rules {
		patterns[i] = r.rule.Pattern
	}

	assert.Contains(t, patterns, "{{.Year}}/{{.Month}}/{{lower .UserEmail}}/*")
	assert.Contains(t, patterns, "archive/{{.Year}}-{{.Month}}-{{.Date}}/*")
	assert.Contains(t, patterns, "users/{{sha2 .UserEmail 16}}/{{upper .UserEmail}}/*")

	// Test that rules can be compiled (basic validation)
	for _, rule := range rules {
		assert.NotNil(t, rule)
		assert.NotEmpty(t, rule.rule.Pattern)
	}
}
