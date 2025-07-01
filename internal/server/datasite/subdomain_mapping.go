package datasite

import (
	"errors"
	"sync"
)

var (
	ErrSubdomainNotFound = errors.New("subdomain not found")
	ErrEmailNotFound     = errors.New("email not found")
)

// VanityDomainConfig stores the configuration for a vanity domain
type VanityDomainConfig struct {
	Email string
	Path  string // Custom path within the datasite (e.g., "/blog", "/portfolio/2024")
}

// SubdomainMapping handles bidirectional mapping between email hashes and emails
type SubdomainMapping struct {
	mu            sync.RWMutex
	hashToEmail   map[string]string
	emailToHash   map[string]string
	vanityDomains map[string]*VanityDomainConfig // maps vanity domains to config
}

// NewSubdomainMapping creates a new subdomain mapping service
func NewSubdomainMapping() *SubdomainMapping {
	return &SubdomainMapping{
		hashToEmail:   make(map[string]string),
		emailToHash:   make(map[string]string),
		vanityDomains: make(map[string]*VanityDomainConfig),
	}
}

// AddMapping adds a mapping between an email and its hash
func (s *SubdomainMapping) AddMapping(email string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if mapping already exists
	if hash, exists := s.emailToHash[email]; exists {
		return hash
	}

	// Generate hash for the email
	hash := EmailToSubdomainHash(email)

	s.hashToEmail[hash] = email
	s.emailToHash[email] = hash

	return hash
}

// GetEmailByHash returns the email for a given hash
func (s *SubdomainMapping) GetEmailByHash(hash string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	email, exists := s.hashToEmail[hash]
	if !exists {
		return "", ErrSubdomainNotFound
	}

	return email, nil
}

// GetHashByEmail returns the hash for a given email
func (s *SubdomainMapping) GetHashByEmail(email string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash, exists := s.emailToHash[email]
	if !exists {
		return "", ErrEmailNotFound
	}

	return hash, nil
}

// RemoveMapping removes a mapping by email
func (s *SubdomainMapping) RemoveMapping(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if hash, exists := s.emailToHash[email]; exists {
		delete(s.hashToEmail, hash)
		delete(s.emailToHash, email)
	}
}

// LoadMappings loads mappings from a list of emails (e.g., from datasites)
func (s *SubdomainMapping) LoadMappings(emails []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, email := range emails {
		hash := EmailToSubdomainHash(email)
		s.hashToEmail[hash] = email
		s.emailToHash[email] = hash
	}
}

// GetAllMappings returns all current mappings
func (s *SubdomainMapping) GetAllMappings() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a copy to avoid race conditions
	result := make(map[string]string, len(s.hashToEmail))
	for hash, email := range s.hashToEmail {
		result[hash] = email
	}

	return result
}

// AddVanityDomain adds a vanity domain mapping
func (s *SubdomainMapping) AddVanityDomain(domain string, email string, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.vanityDomains[domain] = &VanityDomainConfig{
		Email: email,
		Path:  path,
	}
}

// GetVanityDomain returns the configuration for a vanity domain
func (s *SubdomainMapping) GetVanityDomain(domain string) (*VanityDomainConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config, exists := s.vanityDomains[domain]
	return config, exists
}

// RemoveVanityDomain removes a vanity domain mapping
func (s *SubdomainMapping) RemoveVanityDomain(domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.vanityDomains, domain)
}

// ClearVanityDomains clears all vanity domain mappings for a specific email
func (s *SubdomainMapping) ClearVanityDomains(email string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find and remove all vanity domains for this email
	for domain, config := range s.vanityDomains {
		if config.Email == email {
			delete(s.vanityDomains, domain)
		}
	}
}

// GetAllVanityDomains returns all vanity domain mappings
func (s *SubdomainMapping) GetAllVanityDomains() map[string]*VanityDomainConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a copy to avoid race conditions
	result := make(map[string]*VanityDomainConfig, len(s.vanityDomains))
	for domain, config := range s.vanityDomains {
		result[domain] = &VanityDomainConfig{
			Email: config.Email,
			Path:  config.Path,
		}
	}

	return result
}

// GetMapping returns the mapping for a domain (unified method for both hash and vanity domains)
func (s *SubdomainMapping) GetMapping(domain string) *VanityDomainConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if it's a vanity domain
	if config, exists := s.vanityDomains[domain]; exists {
		return config
	}

	return nil
}

// HasDatasite checks if a datasite (email) is already in the mapping
func (s *SubdomainMapping) HasDatasite(email string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.emailToHash[email]
	return exists
}
