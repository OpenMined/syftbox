package datasite

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// EmailToSubdomainHash generates a subdomain-safe hash from an email address
func EmailToSubdomainHash(email string) string {
	// Normalize email to lowercase
	email = strings.ToLower(strings.TrimSpace(email))

	// Create SHA256 hash
	hash := sha256.Sum256([]byte(email))

	// Convert to hex and take first 16 characters for subdomain
	// This provides sufficient uniqueness while keeping subdomain length reasonable
	return hex.EncodeToString(hash[:])[:16]
}
