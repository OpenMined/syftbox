package datasite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
)

var (
	ErrNoSettingsYAML = errors.New("no settings.yaml found")
)

type DatasiteService struct {
	blob             blob.Service
	acl              *acl.ACLService
	subdomainMapping *SubdomainMapping
	domain           string // Main domain for generating hash subdomains
}

func NewDatasiteService(blobSvc blob.Service, aclSvc *acl.ACLService, domain string) *DatasiteService {
	return &DatasiteService{
		blob:             blobSvc,
		acl:              aclSvc,
		subdomainMapping: NewSubdomainMapping(),
		domain:           domain,
	}
}

func (d *DatasiteService) Start(ctx context.Context) error {
	slog.Debug("datasite service start")

	d.blob.OnBlobChange(d.handleBlobChange)

	// Load subdomain mappings
	if err := d.loadDatasiteSubdomains(); err != nil {
		slog.Warn("failed to load subdomain mappings", "error", err)
		// Continue anyway - subdomain feature is optional
	}

	return nil
}

func (d *DatasiteService) Shutdown(ctx context.Context) error {
	slog.Debug("datasite service shutdown")
	return nil
}

func (d *DatasiteService) GetView(user string) []*blob.BlobInfo {
	// First collect all accessible blobs
	blobs, _ := d.blob.Index().List()
	view := make([]*blob.BlobInfo, 0, len(blobs))

	// Filter blobs based on ACL
	for _, blob := range blobs {
		if err := d.acl.CanAccess(
			&acl.User{ID: user},
			&acl.File{Path: blob.Key},
			acl.AccessRead,
		); err == nil {
			view = append(view, blob)
		}
	}
	return view
}

// GetSubdomainMapping returns the subdomain mapping service
func (d *DatasiteService) GetSubdomainMapping() *SubdomainMapping {
	return d.subdomainMapping
}

// loads all datasite emails into the subdomain mapping
func (d *DatasiteService) loadDatasiteSubdomains() error {
	// perhaps maintain a list of datasites in a separate table/db
	// Get all datasites by listing their acls
	blobs, err := d.blob.Index().FilterBySuffix(aclspec.ACLFileName)
	if err != nil {
		return fmt.Errorf("error listing blobs: %w", err)
	}

	// Extract unique datasite names (emails)
	datasiteMap := make(map[string]bool)
	for _, blob := range blobs {
		datasite := GetOwner(blob.Key)
		if datasite != "" {
			datasiteMap[datasite] = true
		}
	}

	// Convert map to slice
	datasites := make([]string, 0, len(datasiteMap))
	for datasite := range datasiteMap {
		datasites = append(datasites, datasite)
	}

	// Load mappings
	d.subdomainMapping.LoadMappings(datasites)

	// Also add default hash mappings for each datasite
	for _, datasite := range datasites {
		// First, add the default hash-based mapping
		d.addDefaultHashMapping(datasite)

		// Then, load the vanity domain configurations
		if err := d.loadVanityDomain(datasite); err != nil && !errors.Is(err, ErrNoSettingsYAML) {
			slog.Warn("failed to load vanity domain", "datasite", datasite, "error", err)
		}
	}

	slog.Info("loaded datasite subdomain mappings", "datasites", len(datasites))

	return nil
}

// loadVanityDomain loads vanity domain configurations from settings.yaml files
func (d *DatasiteService) loadVanityDomain(datasite string) error {
	settingsPath := filepath.Join(datasite, SettingsFileName)

	if _, exists := d.blob.Index().Get(settingsPath); !exists {
		return ErrNoSettingsYAML
	}

	// Try to read settings.yaml
	resp, err := d.blob.Backend().GetObject(context.Background(), settingsPath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Parse vanity domains
	settings, err := ParseSettingsYAML(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to parse settings.yaml: %w", err)
	}

	// Add vanity domains to mapping
	for domain, path := range settings.VanityDomains {
		if !isValidVanityDomainPath(path) {
			slog.Error("invalid vanity domain path", "datasite", datasite, "path", path)
			continue
		}

		// Replace {email-hash} with actual hash
		if domain == "{email-hash}" || domain == "default" {
			hash := EmailToSubdomainHash(datasite)
			domain = hash + "." + d.domain
		}

		// Security check: validate domain ownership
		if !d.isAllowedDomain(domain, datasite) {
			slog.Warn("user tried to claim unauthorized domain",
				"datasite", datasite,
				"domain", domain,
				"action", "rejected")
			continue
		}

		d.subdomainMapping.AddVanityDomain(domain, datasite, path)
		slog.Info("added vanity domain", "datasite", datasite, "domain", domain, "path", path)
	}

	return nil
}

// ReloadVanityDomains reloads vanity domain configurations for a specific email
func (d *DatasiteService) ReloadVanityDomains(email string) error {
	slog.Debug("reloading vanity domain configurations", "email", email)

	// Clear existing vanity domains for this email
	d.subdomainMapping.ClearVanityDomains(email)

	// Re-add the default hash mapping
	d.addDefaultHashMapping(email)

	return d.loadVanityDomain(email)
}

// handleBlobChange handles blob change notifications and reloads settings if needed
func (d *DatasiteService) handleBlobChange(key string, eventType blob.BlobEventType) {
	// ignore events other than put and delete or non-acl files
	if eventType&(blob.BlobEventPut|blob.BlobEventDelete) == 0 || !strings.Contains(key, aclspec.ACLFileName) {
		return
	}

	// Extract the datasite name from the key
	datasite := GetOwner(key)
	if datasite == "" {
		slog.Debug("blob change event for non-datasite key", "key", key)
		return
	}

	// Check if this datasite is already in our mapping
	if !d.subdomainMapping.HasDatasite(datasite) {
		// New datasite detected! Add it to the subdomain mapping
		slog.Info("new datasite detected, adding to subdomain mapping", "datasite", datasite, "key", key)

		if err := d.ReloadVanityDomains(datasite); err != nil {
			if !errors.Is(err, ErrNoSettingsYAML) {
				slog.Warn("failed to reload vanity domain", "datasite", datasite, "error", err)
			}
		}
		return
	}

	// Check if this is a settings.yaml file change
	if key == filepath.Join(datasite, SettingsFileName) {
		slog.Info("settings.yaml changed", "datasite", datasite, "event", eventType)

		// Reload vanity domains for this email
		if err := d.ReloadVanityDomains(datasite); err != nil {
			if !errors.Is(err, ErrNoSettingsYAML) {
				slog.Warn("failed to reload vanity domain", "datasite", datasite, "error", err)
			}
		}
	}
}

// addDefaultHashMapping adds the default hash-based subdomain mapping
func (d *DatasiteService) addDefaultHashMapping(email string) {
	// Only add default mapping if domain is configured
	if d.domain == "" {
		return
	}

	// Generate the hash for this email
	hash := EmailToSubdomainHash(email)

	// Create the default hash-based subdomain (e.g., ff8d9819fc0e12bf.syftbox.local)
	hashDomain := hash + "." + d.domain

	// Map it to /public by default
	d.subdomainMapping.AddMapping(email)
	d.subdomainMapping.AddVanityDomain(hashDomain, email, "/public")
	slog.Debug("added default domain", "datasite", email, "domain", hashDomain, "path", "/public")
}

// isAllowedDomain checks if a user is allowed to claim a domain
func (d *DatasiteService) isAllowedDomain(domain string, email string) bool {
	// Calculate this user's hash
	userHash := EmailToSubdomainHash(email)

	// Check if it's their hash subdomain
	if strings.HasPrefix(domain, userHash+".") {
		// Users can always configure their own hash subdomain
		return true
	}

	// Check if it's the main domain or a subdomain of it
	if d.domain != "" && (domain == d.domain || strings.HasSuffix(domain, "."+d.domain)) {
		// Don't allow claiming the main domain or other hash subdomains
		parts := strings.Split(domain, ".")
		if len(parts) > 0 {
			possibleHash := parts[0]
			// Check if first part looks like a hash (16 hex chars)
			if len(possibleHash) == 16 && isHexString(possibleHash) {
				// This looks like someone else's hash subdomain
				return false
			}
		}
		// Don't allow claiming the base domain
		if domain == d.domain || domain == "www."+d.domain {
			return false
		}
	}

	// For now, allow other domains (like test.local)
	// TODO: Implement domain verification for production
	return true
}
