package datasite

import (
	"testing"
	
	"github.com/stretchr/testify/assert"
)

func TestSubdomainMapping_HasDatasite(t *testing.T) {
	sm := NewSubdomainMapping()
	
	// Test non-existent datasite
	assert.False(t, sm.HasDatasite("test@example.com"))
	
	// Add a datasite
	sm.AddMapping("test@example.com")
	
	// Test existing datasite
	assert.True(t, sm.HasDatasite("test@example.com"))
}

func TestExtractDatasiteName_ValidPaths(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"alice@example.com/public/file.txt", "alice@example.com"},
		{"bob@test.org/private/data.json", "bob@test.org"},
		{"user@domain.co.uk/settings.yaml", "user@domain.co.uk"},
		{"test@email.com", "test@email.com"},
	}
	
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := ExtractDatasiteName(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractDatasiteName_InvalidPaths(t *testing.T) {
	tests := []struct {
		path string
	}{
		{"not-an-email/file.txt"},
		{"file.txt"},
		{"/absolute/path/file.txt"},
		{""},
		{"user@/file.txt"}, // Invalid email
		{"@domain.com/file.txt"}, // Invalid email
	}
	
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := ExtractDatasiteName(tt.path)
			assert.Empty(t, result)
		})
	}
}