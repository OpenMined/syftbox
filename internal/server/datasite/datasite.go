package datasite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
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
	if err := d.LoadDatasiteSubdomains(); err != nil {
		slog.Warn("failed to load subdomain mappings", "error", err)
		// Continue anyway - subdomain feature is optional
	}

	// Fetch the ACL files
	start := time.Now()
	acls, err := d.ListAclFiles()
	if err != nil {
		return fmt.Errorf("error listing acls: %w", err)
	}
	slog.Debug("acl list", "count", len(acls), "took", time.Since(start))

	// Fetch the ACL rulesets
	start = time.Now()
	ruleSets, err := d.fetchAcls(ctx, acls)
	if err != nil {
		return fmt.Errorf("error fetching acls: %w", err)
	}
	slog.Debug("acl fetch", "count", len(ruleSets), "took", time.Since(start))

	if len(ruleSets) == 0 {
		slog.Warn("no ACL rulesets found")
		return nil
	}

	// Load the ACL rulesets
	start = time.Now()
	for _, ruleSet := range ruleSets {
		if _, err := d.acl.AddRuleSet(ruleSet); err != nil {
			slog.Warn("ruleset update error", "path", ruleSet.Path, "error", err)
		}
	}
	slog.Debug("acl build", "count", len(ruleSets), "took", time.Since(start))

	// Warm up the ACL cache
	start = time.Now()
	for blob := range d.blob.Index().Iter() {
		if err := d.acl.CanAccess(
			&acl.User{ID: aclspec.Everyone},
			&acl.File{Path: blob.Key},
			acl.AccessRead,
		); err != nil && errors.Is(err, acl.ErrNoRule) {
			slog.Warn("acl cache warm error", "path", blob.Key, "error", err)
		}
	}
	slog.Debug("acl cache warm", "took", time.Since(start))

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

func (d *DatasiteService) ListAclFiles() ([]*blob.BlobInfo, error) {
	return d.blob.Index().FilterBySuffix(aclspec.AclFileName)
}

func (d *DatasiteService) fetchAcls(ctx context.Context, aclBlobs []*blob.BlobInfo) ([]*aclspec.RuleSet, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := 16
	jobs := make(chan *blob.BlobInfo)
	results := make([]*aclspec.RuleSet, 0, len(aclBlobs))
	blobBackend := d.blob.Backend()

	slog.Debug("acl fetch", "workers", workers, "blobs", len(aclBlobs))

	// Start workers
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for blob := range jobs {

				// Pull the ACL file
				obj, err := blobBackend.GetObject(ctx, blob.Key)
				if err != nil {
					slog.Error("ruleset fetch error", "path", blob.Key, "error", err)
					continue
				}

				// Parse the ACL file
				ruleset, err := aclspec.LoadFromReader(blob.Key, obj.Body)
				obj.Body.Close()
				if err != nil {
					slog.Error("ruleset parse error", "path", blob.Key, "error", err)
					continue
				}

				// Append the ruleset to the results
				mu.Lock()
				results = append(results, ruleset)
				mu.Unlock()
			}
		}()
	}

	// Send work to workers
	for _, blob := range aclBlobs {
		jobs <- blob
	}
	close(jobs)
	wg.Wait()

	return results, nil
}

// GetSubdomainMapping returns the subdomain mapping service
func (d *DatasiteService) GetSubdomainMapping() *SubdomainMapping {
	return d.subdomainMapping
}

// LoadDatasiteSubdomains loads all datasite emails into the subdomain mapping
func (d *DatasiteService) LoadDatasiteSubdomains() error {
	// Get all datasites by listing directories
	blobs, err := d.blob.Index().List()
	if err != nil {
		return fmt.Errorf("error listing blobs: %w", err)
	}

	slog.Debug("LoadDatasiteSubdomains: found blobs", "count", len(blobs))

	// Extract unique datasite names (emails)
	datasiteMap := make(map[string]bool)
	for _, blob := range blobs {
		datasite := GetOwner(blob.Key)
		if datasite != "" {
			datasiteMap[datasite] = true
			slog.Debug("LoadDatasiteSubdomains: extracted datasite", "blob_key", blob.Key, "datasite", datasite)
		}
	}

	slog.Debug("LoadDatasiteSubdomains: unique datasites found", "count", len(datasiteMap))

	// Convert map to slice
	datasites := make([]string, 0, len(datasiteMap))
	for datasite := range datasiteMap {
		datasites = append(datasites, datasite)
	}

	// Load mappings
	slog.Debug("LoadDatasiteSubdomains: loading mappings", "datasites", datasites)
	d.subdomainMapping.LoadMappings(datasites)

	// Also add default hash mappings for each datasite
	for _, datasite := range datasites {
		d.addDefaultHashMapping(datasite)
	}

	slog.Info("loaded datasite subdomain mappings", "count", len(datasites))

	// Load vanity domain configurations
	if err := d.LoadVanityDomains(datasites); err != nil {
		slog.Warn("failed to load vanity domains", "error", err)
		// Continue anyway - vanity domains are optional
	}

	return nil
}

// LoadVanityDomains loads vanity domain configurations from settings.yaml files
func (d *DatasiteService) LoadVanityDomains(datasites []string) error {
	slog.Debug("loading vanity domain configurations", "datasites", len(datasites))

	for _, email := range datasites {
		// First, add the default hash-based mapping
		d.addDefaultHashMapping(email)

		settingsPath := email + "/settings.yaml"

		// Try to read settings.yaml
		resp, err := d.blob.Backend().GetObject(context.Background(), settingsPath)
		if err != nil {
			// File might not exist, which is fine
			slog.Debug("no settings.yaml found", "path", settingsPath)
			continue
		}
		defer resp.Body.Close()

		// Parse vanity domains
		settings, err := ParseSettingsYAML(resp.Body)
		if err != nil {
			slog.Error("failed to parse settings.yaml",
				"path", settingsPath,
				"error", err,
				"action", "skipping vanity domain configuration")
			continue
		}

		// Log parsed domains for debugging
		slog.Debug("parsed vanity domains from settings.yaml",
			"path", settingsPath,
			"domains_count", len(settings.VanityDomains),
			"domains", settings.VanityDomains)

		// Add vanity domains to mapping
		for domain, path := range settings.VanityDomains {
			// Replace {email-hash} with actual hash
			if domain == "{email-hash}" {
				hash := EmailToSubdomainHash(email)
				domain = hash + "." + d.domain
			}

			// Security check: validate domain ownership
			if !d.isAllowedDomain(domain, email) {
				slog.Warn("user tried to claim unauthorized domain",
					"email", email,
					"domain", domain,
					"action", "rejected")
				continue
			}

			d.subdomainMapping.AddVanityDomain(domain, email, path)
			slog.Info("loaded vanity domain", "domain", domain, "email", email, "path", path)
		}
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

	settingsPath := email + "/settings.yaml"

	// Try to read settings.yaml
	resp, err := d.blob.Backend().GetObject(context.Background(), settingsPath)
	if err != nil {
		// File might not exist, which is fine
		slog.Debug("no settings.yaml found", "path", settingsPath)
		return nil
	}
	defer resp.Body.Close()

	// Parse vanity domains
	settings, err := ParseSettingsYAML(resp.Body)
	if err != nil {
		slog.Error("failed to parse settings.yaml during reload",
			"path", settingsPath,
			"error", err,
			"action", "aborting vanity domain reload")
		return err
	}

	// Log parsed domains for debugging
	slog.Debug("parsed vanity domains from settings.yaml during reload",
		"path", settingsPath,
		"domains_count", len(settings.VanityDomains),
		"domains", settings.VanityDomains)

	// Add vanity domains to mapping
	for domain, path := range settings.VanityDomains {
		// Replace {email-hash} with actual hash
		if domain == "{email-hash}" {
			hash := EmailToSubdomainHash(email)
			domain = hash + "." + d.domain
		}

		// Security check: validate domain ownership
		if !d.isAllowedDomain(domain, email) {
			slog.Warn("user tried to claim unauthorized domain",
				"email", email,
				"domain", domain,
				"action", "rejected")
			continue
		}

		d.subdomainMapping.AddVanityDomain(domain, email, path)
		slog.Info("reloaded vanity domain", "domain", domain, "email", email, "path", path)
	}

	return nil
}

// handleBlobChange handles blob change notifications and reloads settings if needed
func (d *DatasiteService) handleBlobChange(key string, eventType blob.BlobEventType) {
	// ignore events other than put
	if eventType != blob.BlobEventPut {
		return
	}

	// Extract the datasite name from the key
	datasiteName := GetOwner(key)
	if datasiteName == "" {
		slog.Debug("blob change event for non-datasite key", "key", key)
		return
	}

	// Check if this datasite is already in our mapping
	if !d.subdomainMapping.HasDatasite(datasiteName) {
		// New datasite detected! Add it to the subdomain mapping
		slog.Info("new datasite detected, adding to subdomain mapping", "datasite", datasiteName, "key", key)

		// Add the default hash mapping for this new datasite
		d.addDefaultHashMapping(datasiteName)

		// Also check if there's already a settings.yaml file for vanity domains
		if err := d.ReloadVanityDomains(datasiteName); err != nil {
			slog.Debug("no vanity domains found for new datasite", "datasite", datasiteName, "error", err)
		}

		slog.Info("added new datasite to subdomain mapping", "datasite", datasiteName)
	}

	// Check if this is a settings.yaml file change
	if strings.HasSuffix(key, "/settings.yaml") {
		slog.Info("settings.yaml changed, reloading vanity domains", "email", datasiteName, "key", key)

		// Reload vanity domains for this email
		if err := d.ReloadVanityDomains(datasiteName); err != nil {
			slog.Warn("failed to reload vanity domains", "email", datasiteName, "error", err)
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
	d.subdomainMapping.AddVanityDomain(hashDomain, email, "/public")
	slog.Debug("added default hash mapping", "domain", hashDomain, "email", email, "path", "/public")
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

// isHexString checks if a string contains only hex characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
