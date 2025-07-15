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
		aclspec.SetTerminal,
		aclspec.NewRule("*.md", aclspec.PublicReadAccess(), aclspec.DefaultLimits()),
		aclspec.NewRule("*.txt", aclspec.PrivateAccess(), aclspec.DefaultLimits()),
	)

	ver, err := service.AddRuleSet(ruleset)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

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
	service := aclSvc()

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
	rule, err := service.GetRule("user1@email.com/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	rule, err = service.GetRule("user2@email.com/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)

	// Remove one ruleset
	removed := service.RemoveRuleSet("user1@email.com")
	assert.True(t, removed)

	// Verify removed ruleset no longer works
	rule, err = service.GetRule("user1@email.com/file.txt")
	assert.Error(t, err)
	assert.Nil(t, rule)

	// Verify other ruleset still works
	rule, err = service.GetRule("user2@email.com/file.txt")
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
	service := aclSvc()

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
	service := aclSvc()

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
	ver, err := service.AddRuleSet(ruleset1)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)

	ver, err = service.AddRuleSet(ruleset2)
	assert.NoError(t, err)
	assert.Equal(t, ACLVersion(1), ver)
	assert.NoError(t, err)

	// Verify both rulesets work
	rule, err := service.GetRule("user1@email.com/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.txt", rule.rule.Pattern)

	rule, err = service.GetRule("user2@email.com/file.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, "*.md", rule.rule.Pattern)
}

func TestAclServiceCacheInvalidation(t *testing.T) {
	service := aclSvc()

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
	rule, err := service.GetRule("user1@email.com/readme.md")
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
	rule, err = service.GetRule("user1@email.com/readme.md")
	assert.NoError(t, err)
	assert.NotNil(t, rule)
	assert.True(t, rule.node.GetTerminal())
	assert.Equal(t, rule.node.GetVersion(), ACLVersion(2))
}
