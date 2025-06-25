package aclspec

import (
	"testing"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewAccess(t *testing.T) {
	// Test creating Access with different user email combinations
	// This tests the core constructor and validates that sets are properly initialized
	admin := []string{"admin@example.com", "owner@company.org"}
	write := []string{"writer1@openmined.org", "writer2@research.edu", "collab@university.ac.uk"}
	read := []string{"reader@public.org"}

	access := NewAccess(admin, write, read)

	// Verify all sets are properly initialized and contain expected user emails
	assert.True(t, access.Admin.Contains("admin@example.com"))
	assert.True(t, access.Admin.Contains("owner@company.org"))
	assert.Equal(t, 2, access.Admin.Cardinality())

	assert.True(t, access.Write.Contains("writer1@openmined.org"))
	assert.True(t, access.Write.Contains("writer2@research.edu"))
	assert.True(t, access.Write.Contains("collab@university.ac.uk"))
	assert.Equal(t, 3, access.Write.Cardinality())

	assert.True(t, access.Read.Contains("reader@public.org"))
	assert.Equal(t, 1, access.Read.Cardinality())
}

func TestNewAccessWithEmptyLists(t *testing.T) {
	// Test edge case of creating Access with empty lists
	// This ensures the constructor handles empty inputs gracefully
	access := NewAccess([]string{}, []string{}, []string{})

	assert.Equal(t, 0, access.Admin.Cardinality())
	assert.Equal(t, 0, access.Write.Cardinality())
	assert.Equal(t, 0, access.Read.Cardinality())
}

func TestPrivateAccess(t *testing.T) {
	// Test that PrivateAccess creates an Access object with no permissions
	// This is a critical security test - private should mean NO access for anyone
	access := PrivateAccess()

	assert.Equal(t, 0, access.Admin.Cardinality(), "Private access should have no admin users")
	assert.Equal(t, 0, access.Write.Cardinality(), "Private access should have no write users")
	assert.Equal(t, 0, access.Read.Cardinality(), "Private access should have no read users")
}

func TestPublicReadAccess(t *testing.T) {
	// Test that PublicReadAccess grants read access to everyone but nothing else
	// This validates the most common public sharing scenario
	access := PublicReadAccess()

	assert.Equal(t, 0, access.Admin.Cardinality(), "Public read should have no admin users")
	assert.Equal(t, 0, access.Write.Cardinality(), "Public read should have no write users")
	assert.Equal(t, 1, access.Read.Cardinality(), "Public read should have exactly one read entry")
	assert.True(t, access.Read.Contains(Everyone), "Public read should grant read access to everyone")
}

func TestPublicReadWriteAccess(t *testing.T) {
	// Test that PublicReadWriteAccess grants write (and implicitly read) access to everyone
	// This tests the dangerous but sometimes necessary full public access
	access := PublicReadWriteAccess()

	assert.Equal(t, 0, access.Admin.Cardinality(), "Public read-write should have no admin users")
	assert.Equal(t, 1, access.Write.Cardinality(), "Public read-write should have exactly one write entry")
	assert.True(t, access.Write.Contains(Everyone), "Public read-write should grant write access to everyone")
	assert.Equal(t, 0, access.Read.Cardinality(), "Public read-write should not set read (write implies read)")
}

func TestSharedReadAccess(t *testing.T) {
	// Test creating shared read access for specific user emails
	// This validates the common scenario of sharing read access with specific collaborators
	users := []string{"alice@research.org", "bob@university.edu", "charlie@company.com"}
	access := SharedReadAccess(users...)

	assert.Equal(t, 0, access.Admin.Cardinality(), "Shared read should have no admin users")
	assert.Equal(t, 0, access.Write.Cardinality(), "Shared read should have no write users")
	assert.Equal(t, 3, access.Read.Cardinality(), "Shared read should have exactly the specified users")
	
	for _, user := range users {
		assert.True(t, access.Read.Contains(user), "Shared read should contain user %s", user)
	}
}

func TestSharedReadWriteAccess(t *testing.T) {
	// Test creating shared read-write access for specific user emails
	// This validates collaborative scenarios where specific users need write access
	users := []string{"maintainer1@openmined.org", "maintainer2@research.edu"}
	access := SharedReadWriteAccess(users...)

	assert.Equal(t, 0, access.Admin.Cardinality(), "Shared read-write should have no admin users")
	assert.Equal(t, 2, access.Write.Cardinality(), "Shared read-write should have exactly the specified users")
	assert.Equal(t, 0, access.Read.Cardinality(), "Shared read-write should not set read (write implies read)")
	
	for _, user := range users {
		assert.True(t, access.Write.Contains(user), "Shared read-write should contain user %s", user)
	}
}

func TestAccessUnmarshalYAML(t *testing.T) {
	// Test unmarshaling Access from YAML format
	// This is critical for loading syft.pub.yaml files from disk
	yamlData := `
admin: ["admin@example.com", "owner@company.org"]
read: ["reader1@public.org", "reader2@university.edu", "reader3@research.net"]
write: ["writer@openmined.org"]
`

	var access Access
	err := yaml.Unmarshal([]byte(yamlData), &access)
	require.NoError(t, err, "YAML unmarshaling should succeed")

	// Verify that all user emails were properly loaded into their respective sets
	assert.Equal(t, 2, access.Admin.Cardinality())
	assert.True(t, access.Admin.Contains("admin@example.com"))
	assert.True(t, access.Admin.Contains("owner@company.org"))

	assert.Equal(t, 3, access.Read.Cardinality())
	assert.True(t, access.Read.Contains("reader1@public.org"))
	assert.True(t, access.Read.Contains("reader2@university.edu"))
	assert.True(t, access.Read.Contains("reader3@research.net"))

	assert.Equal(t, 1, access.Write.Cardinality())
	assert.True(t, access.Write.Contains("writer@openmined.org"))
}

func TestAccessUnmarshalYAMLWithMissingSections(t *testing.T) {
	// Test unmarshaling when some access sections are missing
	// This ensures the system handles partial configurations gracefully
	yamlData := `
read: ["reader@example.com"]
# write and admin sections intentionally missing
`

	var access Access
	err := yaml.Unmarshal([]byte(yamlData), &access)
	require.NoError(t, err, "YAML unmarshaling should succeed even with missing sections")

	// Missing sections should result in empty sets, not nil
	assert.Equal(t, 0, access.Admin.Cardinality(), "Missing admin section should result in empty set")
	assert.Equal(t, 0, access.Write.Cardinality(), "Missing write section should result in empty set")
	assert.Equal(t, 1, access.Read.Cardinality(), "Read section should be properly loaded")
	assert.True(t, access.Read.Contains("reader@example.com"))
}

func TestAccessUnmarshalYAMLWithEmptyLists(t *testing.T) {
	// Test unmarshaling with explicitly empty lists
	// This ensures empty lists in YAML are handled correctly
	yamlData := `
admin: []
read: []
write: ["writer@company.com"]
`

	var access Access
	err := yaml.Unmarshal([]byte(yamlData), &access)
	require.NoError(t, err, "YAML unmarshaling should succeed with empty lists")

	assert.Equal(t, 0, access.Admin.Cardinality())
	assert.Equal(t, 0, access.Read.Cardinality())
	assert.Equal(t, 1, access.Write.Cardinality())
	assert.True(t, access.Write.Contains("writer@company.com"))
}

func TestAccessUnmarshalYAMLInvalidFormat(t *testing.T) {
	// Test that invalid YAML format is properly rejected
	// This ensures the system fails safely on malformed input
	yamlData := `
admin: "should_be_a_list_not_string"
`

	var access Access
	err := yaml.Unmarshal([]byte(yamlData), &access)
	assert.Error(t, err, "Invalid YAML format should be rejected")
}

func TestAccessMarshalYAML(t *testing.T) {
	// Test marshaling Access to YAML format
	// This is critical for saving syft.pub.yaml files to disk
	access := NewAccess(
		[]string{"admin@example.com", "owner@company.org"},
		[]string{"writer@openmined.org"},
		[]string{"reader1@public.org", "reader2@university.edu"},
	)

	data, err := yaml.Marshal(access)
	require.NoError(t, err, "YAML marshaling should succeed")

	// Unmarshal back to verify the round-trip works correctly
	var unmarshaled Access
	err = yaml.Unmarshal(data, &unmarshaled)
	require.NoError(t, err, "Round-trip unmarshaling should succeed")

	// Verify all data survived the round-trip
	assert.Equal(t, access.Admin.Cardinality(), unmarshaled.Admin.Cardinality())
	assert.Equal(t, access.Write.Cardinality(), unmarshaled.Write.Cardinality())
	assert.Equal(t, access.Read.Cardinality(), unmarshaled.Read.Cardinality())

	// Check that all user emails are preserved
	for _, user := range access.Admin.ToSlice() {
		assert.True(t, unmarshaled.Admin.Contains(user))
	}
	for _, user := range access.Write.ToSlice() {
		assert.True(t, unmarshaled.Write.Contains(user))
	}
	for _, user := range access.Read.ToSlice() {
		assert.True(t, unmarshaled.Read.Contains(user))
	}
}

func TestAccessMarshalYAMLWithNilSets(t *testing.T) {
	// Test marshaling when some sets are nil
	// This tests the defensive programming in MarshalYAML
	access := Access{
		Admin: mapset.NewSet("admin@example.com"),
		Write: nil, // Intentionally nil
		Read:  mapset.NewSet("reader@public.org"),
	}

	data, err := yaml.Marshal(access)
	require.NoError(t, err, "YAML marshaling should handle nil sets gracefully")

	// Verify the marshaled data is valid YAML
	var result map[string][]string
	err = yaml.Unmarshal(data, &result)
	require.NoError(t, err, "Marshaled YAML should be valid")

	// Nil sets should not appear in the output
	assert.Contains(t, result, "admin")
	assert.Contains(t, result, "read")
	assert.NotContains(t, result, "write", "Nil sets should not appear in marshaled YAML")
}

func TestAccessMarshalYAMLWithEmptySets(t *testing.T) {
	// Test marshaling when sets are empty but not nil
	// This verifies that empty sets are handled consistently
	access := NewAccess(
		[]string{}, // Empty admin
		[]string{}, // Empty write
		[]string{"reader@example.com"}, // Non-empty read
	)

	data, err := yaml.Marshal(access)
	require.NoError(t, err, "YAML marshaling should handle empty sets")

	var result map[string][]string
	err = yaml.Unmarshal(data, &result)
	require.NoError(t, err, "Marshaled YAML should be valid")

	// Empty sets should appear as empty arrays in YAML
	assert.Equal(t, []string{}, result["admin"])
	assert.Equal(t, []string{}, result["write"])
	assert.Contains(t, result["read"], "reader@example.com")
}