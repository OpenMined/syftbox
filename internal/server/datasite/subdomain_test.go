package datasite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailToSubdomainHash(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{
			email:    "alice@example.com",
			expected: "ff8d9819fc0e12bf", // First 16 chars of SHA256 hash
		},
		{
			email:    "ALICE@EXAMPLE.COM", // Should be case-insensitive
			expected: "ff8d9819fc0e12bf",
		},
		{
			email:    "  alice@example.com  ", // Should trim spaces
			expected: "ff8d9819fc0e12bf",
		},
		{
			email:    "bob@example.com",
			expected: "5ff860bf1190596c", // Different hash
		},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			hash := EmailToSubdomainHash(tt.email)
			assert.Equal(t, tt.expected, hash)
			assert.Equal(t, 16, len(hash)) // Hash should always be 16 characters
		})
	}
}
