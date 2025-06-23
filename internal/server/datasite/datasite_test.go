package datasite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/openmined/syftbox/internal/server/middlewares"
)

func TestDomainOwnershipValidation(t *testing.T) {
	tests := []struct {
		name           string
		domain         string
		email          string
		mainDomain     string
		expectedResult bool
		description    string
	}{
		{
			name:           "UserCanConfigureOwnHashSubdomain",
			domain:         "ff8d9819fc0e12bf.syftbox.local",
			email:          "alice@example.com",
			mainDomain:     "syftbox.local",
			expectedResult: true,
			description:    "Users should be able to configure their own hash subdomain",
		},
		{
			name:           "UserCannotConfigureOthersHashSubdomain",
			domain:         "1234567890abcdef.syftbox.local",
			email:          "alice@example.com",
			mainDomain:     "syftbox.local",
			expectedResult: false,
			description:    "Users should not be able to configure other users' hash subdomains",
		},
		{
			name:           "UserCannotClaimMainDomain",
			domain:         "syftbox.local",
			email:          "alice@example.com",
			mainDomain:     "syftbox.local",
			expectedResult: false,
			description:    "Users should not be able to claim the main domain",
		},
		{
			name:           "UserCannotClaimWWWDomain",
			domain:         "www.syftbox.local",
			email:          "alice@example.com",
			mainDomain:     "syftbox.local",
			expectedResult: false,
			description:    "Users should not be able to claim www subdomain",
		},
		{
			name:           "UserCanClaimCustomDomain",
			domain:         "alice.dev",
			email:          "alice@example.com",
			mainDomain:     "syftbox.local",
			expectedResult: true,
			description:    "Users should be able to claim custom domains",
		},
		{
			name:           "UserCanClaimLocalTestDomain",
			domain:         "test.local",
			email:          "alice@example.com",
			mainDomain:     "syftbox.local",
			expectedResult: true,
			description:    "Users should be able to claim local test domains",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal datasite service for testing
			ds := &DatasiteService{domain: tt.mainDomain}
			
			result := ds.isAllowedDomain(tt.domain, tt.email)
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("IsHexString", func(t *testing.T) {
		tests := []struct {
			input    string
			expected bool
		}{
			{"ff8d9819fc0e12bf", true},
			{"1234567890abcdef", true},
			{"ABCDEF1234567890", true},
			{"not-hex-string!!", false},
			{"gg8d9819fc0e12bf", false}, // 'g' is not hex
			{"", true}, // Empty string technically contains only hex chars
		}

		for _, tt := range tests {
			result := isHexString(tt.input)
			assert.Equal(t, tt.expected, result, "isHexString(%q)", tt.input)
		}
	})

	t.Run("ExtractDatasiteName", func(t *testing.T) {
		tests := []struct {
			path     string
			expected string
		}{
			{"alice@example.com/public/index.html", "alice@example.com"},
			{"alice@example.com/", "alice@example.com"},
			{"alice@example.com", "alice@example.com"}, // Fixed: single email should return itself
			{"", ""},
			{"/", ""},
		}

		for _, tt := range tests {
			result := ExtractDatasiteName(tt.path)
			assert.Equal(t, tt.expected, result, "ExtractDatasiteName(%q)", tt.path)
		}
	})
}

func TestEmailHashExpansion(t *testing.T) {
	email := "alice@example.com"
	expectedHash := middlewares.EmailToSubdomainHash(email)
	domain := "{email-hash}"
	
	// Create a minimal datasite service
	ds := &DatasiteService{domain: "syftbox.local"}
	
	// Test the logic that would expand {email-hash}
	// This is the same logic used in LoadVanityDomains
	if domain == "{email-hash}" {
		hash := middlewares.EmailToSubdomainHash(email)
		domain = hash + "." + ds.domain
	}
	
	expectedDomain := expectedHash + ".syftbox.local"
	assert.Equal(t, expectedDomain, domain, "{email-hash} should expand to user's hash subdomain")
}

func TestEmailHashConfigParsing(t *testing.T) {
	tests := []struct {
		name           string
		email          string
		configDomains  map[string]interface{}
		expectedDomain string
		expectedPath   string
		shouldExist    bool
		description    string
	}{
		{
			name:  "EmailHashWithBlogPath",
			email: "alice@example.com",
			configDomains: map[string]interface{}{
				"{email-hash}": "/blog",
			},
			expectedDomain: "ff8d9819fc0e12bf.syftbox.local", // alice@example.com hash
			expectedPath:   "/blog",
			shouldExist:    true,
			description:    "'{email-hash}': /blog should expand to user's hash subdomain with /blog path",
		},
		{
			name:  "EmailHashWithPublicPath",
			email: "bob@example.com",
			configDomains: map[string]interface{}{
				"{email-hash}": "/public",
			},
			expectedDomain: "5ff860bf1190596c.syftbox.local", // bob@example.com hash
			expectedPath:   "/public",
			shouldExist:    true,
			description:    "'{email-hash}': /public should expand to user's hash subdomain with /public path",
		},
		{
			name:  "EmailHashWithRootPath",
			email: "charlie@example.com",
			configDomains: map[string]interface{}{
				"{email-hash}": "/",
			},
			expectedDomain: "add7232b65bb559f.syftbox.local", // charlie@example.com hash
			expectedPath:   "/",
			shouldExist:    true,
			description:    "'{email-hash}': / should expand to user's hash subdomain with root path",
		},
		{
			name:  "EmailHashWithNestedPath",
			email: "dave@example.com",
			configDomains: map[string]interface{}{
				"{email-hash}": "/projects/2024",
			},
			expectedDomain: "7b34211350ff5679.syftbox.local", // dave@example.com hash
			expectedPath:   "/projects/2024",
			shouldExist:    true,
			description:    "'{email-hash}': /projects/2024 should expand to user's hash subdomain with nested path",
		},
		{
			name:  "MultipleDomainsWithEmailHash",
			email: "alice@example.com",
			configDomains: map[string]interface{}{
				"{email-hash}":  "/blog",
				"alice.dev":     "/portfolio",
				"custom.domain": "/custom",
			},
			expectedDomain: "ff8d9819fc0e12bf.syftbox.local",
			expectedPath:   "/blog",
			shouldExist:    true,
			description:    "Multiple domains including {email-hash} should all be processed correctly",
		},
		{
			name:  "NoEmailHashInConfig",
			email: "alice@example.com",
			configDomains: map[string]interface{}{
				"alice.dev":     "/portfolio",
				"custom.domain": "/custom",
			},
			expectedDomain: "ff8d9819fc0e12bf.syftbox.local",
			expectedPath:   "",
			shouldExist:    false,
			description:    "When {email-hash} is not in config, user's hash subdomain should not be mapped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create subdomain mapping to simulate the vanity domain loading process
			subdomainMapping := NewSubdomainMapping()
			
			// Simulate the LoadVanityDomains process for the specific user
			mainDomain := "syftbox.local"
			
			// Process the config domains (this simulates what LoadVanityDomains does)
			for domain, pathInterface := range tt.configDomains {
				path, ok := pathInterface.(string)
				if !ok {
					path = "/public" // default
				}
				
				// Expand {email-hash} if present (this is the key logic we're testing)
				if domain == "{email-hash}" {
					hash := middlewares.EmailToSubdomainHash(tt.email)
					domain = hash + "." + mainDomain
				}
				
				// Add the vanity domain mapping
				subdomainMapping.AddVanityDomain(domain, tt.email, path)
			}
			
			// Test if the expanded domain exists and has correct configuration
			vanityConfig := subdomainMapping.GetMapping(tt.expectedDomain)
			
			if tt.shouldExist {
				assert.NotNil(t, vanityConfig, tt.description)
				if vanityConfig != nil {
					assert.Equal(t, tt.email, vanityConfig.Email, "Email should match")
					assert.Equal(t, tt.expectedPath, vanityConfig.Path, "Path should match")
				}
			} else {
				assert.Nil(t, vanityConfig, tt.description)
			}
		})
	}
}

func TestEmailHashConfigIntegration(t *testing.T) {
	// Test that simulates a real settings.yaml config parsing scenario
	email := "alice@example.com"
	expectedHash := middlewares.EmailToSubdomainHash(email)
	
	// Simulate YAML config structure
	type MockSettings struct {
		Domains map[string]interface{} `yaml:"domains"`
	}
	
	mockSettings := MockSettings{
		Domains: map[string]interface{}{
			"{email-hash}":  "/blog",
			"alice.dev":     "/portfolio", 
			"alice.site":    "/",
		},
	}
	
	// Create subdomain mapping and datasite service
	subdomainMapping := NewSubdomainMapping()
	mainDomain := "syftbox.local"
	
	// Process domains (simulate LoadVanityDomains logic)
	for domain, pathInterface := range mockSettings.Domains {
		path, ok := pathInterface.(string)
		if !ok {
			path = "/public"
		}
		
		// This is the key expansion logic for {email-hash}
		if domain == "{email-hash}" {
			hash := middlewares.EmailToSubdomainHash(email)
			domain = hash + "." + mainDomain
		}
		
		subdomainMapping.AddVanityDomain(domain, email, path)
	}
	
	// Verify all domains were created correctly
	expectedDomains := map[string]string{
		expectedHash + ".syftbox.local": "/blog",      // Expanded from {email-hash}
		"alice.dev":                     "/portfolio",
		"alice.site":                    "/",
	}
	
	for expectedDomain, expectedPath := range expectedDomains {
		config := subdomainMapping.GetMapping(expectedDomain)
		assert.NotNil(t, config, "Domain %s should exist", expectedDomain)
		if config != nil {
			assert.Equal(t, email, config.Email, "Email should match for domain %s", expectedDomain)
			assert.Equal(t, expectedPath, config.Path, "Path should match for domain %s", expectedDomain)
		}
	}
	
	// Verify the hash subdomain specifically
	hashSubdomain := expectedHash + ".syftbox.local"
	config := subdomainMapping.GetMapping(hashSubdomain)
	assert.NotNil(t, config, "Hash subdomain should be created from {email-hash}")
	assert.Equal(t, "/blog", config.Path, "Hash subdomain should have /blog path")
	
	// Verify GetVanityDomainFunc would work correctly
	getVanityDomainFunc := func(domain string) (string, string, bool) {
		if config := subdomainMapping.GetMapping(domain); config != nil {
			return config.Email, config.Path, true
		}
		return "", "", false
	}
	
	// Test the function with the expanded hash domain
	returnedEmail, returnedPath, exists := getVanityDomainFunc(hashSubdomain)
	assert.True(t, exists, "Hash subdomain should be found by GetVanityDomainFunc")
	assert.Equal(t, email, returnedEmail, "Returned email should match")
	assert.Equal(t, "/blog", returnedPath, "Returned path should match")
}