package datasite

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubdomainMapping(t *testing.T) {
	t.Run("AddMapping", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add a mapping
		hash := sm.AddMapping("alice@example.com")
		assert.NotEmpty(t, hash)
		assert.Equal(t, 16, len(hash)) // Hash should be 16 characters
		
		// Adding the same email should return the same hash
		hash2 := sm.AddMapping("alice@example.com")
		assert.Equal(t, hash, hash2)
		
		// Different email should get different hash
		hash3 := sm.AddMapping("bob@example.com")
		assert.NotEqual(t, hash, hash3)
	})

	t.Run("GetEmailByHash", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add mappings
		hash1 := sm.AddMapping("alice@example.com")
		hash2 := sm.AddMapping("bob@example.com")
		
		// Test valid lookups
		email1, err := sm.GetEmailByHash(hash1)
		assert.NoError(t, err)
		assert.Equal(t, "alice@example.com", email1)
		
		email2, err := sm.GetEmailByHash(hash2)
		assert.NoError(t, err)
		assert.Equal(t, "bob@example.com", email2)
		
		// Test invalid lookup
		_, err = sm.GetEmailByHash("invalid")
		assert.ErrorIs(t, err, ErrSubdomainNotFound)
	})

	t.Run("GetHashByEmail", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add mappings
		expectedHash := sm.AddMapping("alice@example.com")
		
		// Test valid lookup
		hash, err := sm.GetHashByEmail("alice@example.com")
		assert.NoError(t, err)
		assert.Equal(t, expectedHash, hash)
		
		// Test invalid lookup
		_, err = sm.GetHashByEmail("nonexistent@example.com")
		assert.ErrorIs(t, err, ErrEmailNotFound)
	})

	t.Run("RemoveMapping", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add and remove mapping
		hash := sm.AddMapping("alice@example.com")
		sm.RemoveMapping("alice@example.com")
		
		// Verify both lookups fail
		_, err := sm.GetEmailByHash(hash)
		assert.ErrorIs(t, err, ErrSubdomainNotFound)
		
		_, err = sm.GetHashByEmail("alice@example.com")
		assert.ErrorIs(t, err, ErrEmailNotFound)
	})

	t.Run("LoadMappings", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		emails := []string{
			"alice@example.com",
			"bob@example.com",
			"charlie@example.com",
		}
		
		sm.LoadMappings(emails)
		
		// Verify all mappings exist
		for _, email := range emails {
			hash, err := sm.GetHashByEmail(email)
			assert.NoError(t, err)
			assert.NotEmpty(t, hash)
			
			retrievedEmail, err := sm.GetEmailByHash(hash)
			assert.NoError(t, err)
			assert.Equal(t, email, retrievedEmail)
		}
	})

	t.Run("GetAllMappings", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add some mappings
		hash1 := sm.AddMapping("alice@example.com")
		hash2 := sm.AddMapping("bob@example.com")
		
		// Get all mappings
		mappings := sm.GetAllMappings()
		assert.Len(t, mappings, 2)
		assert.Equal(t, "alice@example.com", mappings[hash1])
		assert.Equal(t, "bob@example.com", mappings[hash2])
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Test concurrent reads and writes
		var wg sync.WaitGroup
		numGoroutines := 10
		
		// Concurrent adds
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				email := string(rune('a'+i)) + "@example.com"
				sm.AddMapping(email)
			}(i)
		}
		
		// Concurrent reads
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				email := string(rune('a'+i)) + "@example.com"
				hash, _ := sm.GetHashByEmail(email)
				sm.GetEmailByHash(hash)
			}(i)
		}
		
		// Concurrent get all
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sm.GetAllMappings()
			}()
		}
		
		wg.Wait()
		
		// Verify all mappings exist
		mappings := sm.GetAllMappings()
		assert.GreaterOrEqual(t, len(mappings), numGoroutines)
	})
}

func TestVanityDomainMapping(t *testing.T) {
	t.Run("AddVanityDomain", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add vanity domain mapping
		sm.AddVanityDomain("alice.syftbox.local", "alice@example.com", "/blog")
		
		// Retrieve vanity domain
		config, exists := sm.GetVanityDomain("alice.syftbox.local")
		assert.True(t, exists)
		assert.NotNil(t, config)
		assert.Equal(t, "alice@example.com", config.Email)
		assert.Equal(t, "/blog", config.Path)
		
		// Non-existent domain
		_, exists = sm.GetVanityDomain("nonexistent.local")
		assert.False(t, exists)
	})

	t.Run("VanityDomainWithDefaultPath", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add vanity domain with empty path (should default to /public)
		sm.AddVanityDomain("alice.syftbox.local", "alice@example.com", "")
		
		config, exists := sm.GetVanityDomain("alice.syftbox.local")
		assert.True(t, exists)
		assert.Equal(t, "", config.Path) // Empty path means use default
	})

	t.Run("MultipleVanityDomains", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add multiple vanity domains for same user
		sm.AddVanityDomain("blog.alice.local", "alice@example.com", "/blog")
		sm.AddVanityDomain("portfolio.alice.local", "alice@example.com", "/portfolio")
		sm.AddVanityDomain("projects.alice.local", "alice@example.com", "/projects/2024")
		
		// Verify all exist
		config1, exists1 := sm.GetVanityDomain("blog.alice.local")
		assert.True(t, exists1)
		assert.Equal(t, "/blog", config1.Path)
		
		config2, exists2 := sm.GetVanityDomain("portfolio.alice.local")
		assert.True(t, exists2)
		assert.Equal(t, "/portfolio", config2.Path)
		
		config3, exists3 := sm.GetVanityDomain("projects.alice.local")
		assert.True(t, exists3)
		assert.Equal(t, "/projects/2024", config3.Path)
	})

	t.Run("RemoveVanityDomain", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add and remove vanity domain
		sm.AddVanityDomain("alice.syftbox.local", "alice@example.com", "/blog")
		sm.RemoveVanityDomain("alice.syftbox.local")
		
		// Verify it's gone
		_, exists := sm.GetVanityDomain("alice.syftbox.local")
		assert.False(t, exists)
	})

	t.Run("ClearVanityDomains", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add vanity domains for multiple users
		sm.AddVanityDomain("alice1.local", "alice@example.com", "/blog")
		sm.AddVanityDomain("alice2.local", "alice@example.com", "/portfolio")
		sm.AddVanityDomain("bob.local", "bob@example.com", "/site")
		
		// Clear Alice's domains
		sm.ClearVanityDomains("alice@example.com")
		
		// Verify Alice's domains are gone
		_, exists1 := sm.GetVanityDomain("alice1.local")
		assert.False(t, exists1)
		_, exists2 := sm.GetVanityDomain("alice2.local")
		assert.False(t, exists2)
		
		// Verify Bob's domain remains
		config, exists := sm.GetVanityDomain("bob.local")
		assert.True(t, exists)
		assert.Equal(t, "bob@example.com", config.Email)
	})

	t.Run("GetAllVanityDomains", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add some vanity domains
		sm.AddVanityDomain("alice.local", "alice@example.com", "/blog")
		sm.AddVanityDomain("bob.local", "bob@example.com", "/site")
		
		// Get all domains
		domains := sm.GetAllVanityDomains()
		assert.Len(t, domains, 2)
		
		// Verify content
		assert.Equal(t, "alice@example.com", domains["alice.local"].Email)
		assert.Equal(t, "/blog", domains["alice.local"].Path)
		assert.Equal(t, "bob@example.com", domains["bob.local"].Email)
		assert.Equal(t, "/site", domains["bob.local"].Path)
	})

	t.Run("VanityDomainOverwrite", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add vanity domain
		sm.AddVanityDomain("alice.local", "alice@example.com", "/old-path")
		
		// Overwrite with new path
		sm.AddVanityDomain("alice.local", "alice@example.com", "/new-path")
		
		// Verify new path is used
		config, exists := sm.GetVanityDomain("alice.local")
		assert.True(t, exists)
		assert.Equal(t, "/new-path", config.Path)
	})

	t.Run("VanityDomainOwnershipTracking", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add vanity domain for alice
		sm.AddVanityDomain("cool.domain", "alice@example.com", "/site")
		
		// Try to claim same domain for bob (should overwrite)
		sm.AddVanityDomain("cool.domain", "bob@example.com", "/blog")
		
		// Verify bob owns it now
		config, exists := sm.GetVanityDomain("cool.domain")
		assert.True(t, exists)
		assert.Equal(t, "bob@example.com", config.Email)
		assert.Equal(t, "/blog", config.Path)
	})

	t.Run("ConcurrentVanityDomainAccess", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Test concurrent reads and writes
		var wg sync.WaitGroup
		numGoroutines := 10
		
		// Concurrent adds
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				domain := string(rune('a'+i)) + ".local"
				email := string(rune('a'+i)) + "@example.com"
				sm.AddVanityDomain(domain, email, "/path")
			}(i)
		}
		
		// Concurrent reads
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				domain := string(rune('a'+i)) + ".local"
				sm.GetVanityDomain(domain)
			}(i)
		}
		
		// Concurrent get all
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sm.GetAllVanityDomains()
			}()
		}
		
		wg.Wait()
		
		// Verify all mappings exist
		domains := sm.GetAllVanityDomains()
		assert.GreaterOrEqual(t, len(domains), numGoroutines)
	})
}

func TestHashSubdomainIntegration(t *testing.T) {
	t.Run("HashAndVanityCoexistence", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add regular hash mapping
		hash := sm.AddMapping("alice@example.com")
		
		// Add vanity domain for same user
		sm.AddVanityDomain("alice.blog", "alice@example.com", "/blog")
		
		// Both should work independently
		email, err := sm.GetEmailByHash(hash)
		require.NoError(t, err)
		assert.Equal(t, "alice@example.com", email)
		
		config, exists := sm.GetVanityDomain("alice.blog")
		assert.True(t, exists)
		assert.Equal(t, "alice@example.com", config.Email)
	})

	t.Run("RemoveMappingDoesNotAffectVanity", func(t *testing.T) {
		sm := NewSubdomainMapping()
		
		// Add both mappings
		sm.AddMapping("alice@example.com")
		sm.AddVanityDomain("alice.blog", "alice@example.com", "/blog")
		
		// Remove hash mapping
		sm.RemoveMapping("alice@example.com")
		
		// Hash mapping should be gone
		_, err := sm.GetHashByEmail("alice@example.com")
		assert.ErrorIs(t, err, ErrEmailNotFound)
		
		// Vanity domain should still exist
		config, exists := sm.GetVanityDomain("alice.blog")
		assert.True(t, exists)
		assert.Equal(t, "alice@example.com", config.Email)
	})
}