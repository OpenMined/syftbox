package sync

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/syftmsg"
)

type ACLManifestGenerator struct {
	datasitesDir string
}

func NewACLManifestGenerator(datasitesDir string) *ACLManifestGenerator {
	return &ACLManifestGenerator{datasitesDir: datasitesDir}
}

type aclInfo struct {
	path    string
	hash    string
	readers []string
	writers []string
}

func (g *ACLManifestGenerator) GenerateManifests(datasite string) (map[string]*syftmsg.ACLManifest, error) {
	datasitePath := filepath.Join(g.datasitesDir, datasite)

	acls, err := g.scanACLFiles(datasitePath, datasite)
	if err != nil {
		return nil, fmt.Errorf("scan acl files: %w", err)
	}

	if len(acls) == 0 {
		return nil, nil
	}

	principals := g.collectPrincipals(acls)

	manifests := make(map[string]*syftmsg.ACLManifest)
	for principal := range principals {
		manifest := g.buildManifestForPrincipal(datasite, principal, acls)
		if manifest != nil && len(manifest.ACLOrder) > 0 {
			manifests[syftmsg.HashPrincipal(principal)] = manifest
		}
	}

	return manifests, nil
}

func (g *ACLManifestGenerator) scanACLFiles(datasitePath, datasite string) ([]aclInfo, error) {
	var acls []aclInfo

	err := filepath.Walk(datasitePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if !aclspec.IsACLFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(g.datasitesDir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		hash := fmt.Sprintf("%x", md5.Sum(content))

		ruleset, err := aclspec.LoadFromFile(path)
		if err != nil {
			return nil
		}

		readers := make([]string, 0)
		writers := make([]string, 0)

		for _, rule := range ruleset.AllRules() {
			if rule.Access != nil {
				for r := range rule.Access.Read.Iter() {
					readers = append(readers, r)
				}
				for w := range rule.Access.Write.Iter() {
					writers = append(writers, w)
				}
				for a := range rule.Access.Admin.Iter() {
					writers = append(writers, a)
				}
			}
		}

		aclDir := filepath.Dir(relPath)

		acls = append(acls, aclInfo{
			path:    aclDir,
			hash:    hash,
			readers: readers,
			writers: writers,
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(acls, func(i, j int) bool {
		return acls[i].path < acls[j].path
	})

	return acls, nil
}

func (g *ACLManifestGenerator) collectPrincipals(acls []aclInfo) map[string]struct{} {
	principals := make(map[string]struct{})

	principals["*"] = struct{}{}

	for _, acl := range acls {
		for _, r := range acl.readers {
			principals[r] = struct{}{}
		}
		for _, w := range acl.writers {
			principals[w] = struct{}{}
		}
	}

	return principals
}

func (g *ACLManifestGenerator) buildManifestForPrincipal(datasite, principal string, acls []aclInfo) *syftmsg.ACLManifest {
	var entries []syftmsg.ACLEntry

	for _, acl := range acls {
		if g.principalHasAccess(principal, acl) {
			entries = append(entries, syftmsg.ACLEntry{
				Path: acl.path,
				Hash: acl.hash,
			})
		}
	}

	if len(entries) == 0 {
		return nil
	}

	entries = g.sortTopologically(entries)

	return syftmsg.NewACLManifest(datasite, principal, entries)
}

func (g *ACLManifestGenerator) principalHasAccess(principal string, acl aclInfo) bool {
	for _, r := range acl.readers {
		if r == "*" || r == principal {
			return true
		}
	}
	for _, w := range acl.writers {
		if w == "*" || w == principal {
			return true
		}
	}
	return false
}

func (g *ACLManifestGenerator) sortTopologically(entries []syftmsg.ACLEntry) []syftmsg.ACLEntry {
	sort.Slice(entries, func(i, j int) bool {
		depthI := strings.Count(entries[i].Path, "/")
		depthJ := strings.Count(entries[j].Path, "/")
		if depthI != depthJ {
			return depthI < depthJ
		}
		return entries[i].Path < entries[j].Path
	})
	return entries
}

func (g *ACLManifestGenerator) GenerateManifestForPrincipal(datasite, principal string) (*syftmsg.ACLManifest, error) {
	manifests, err := g.GenerateManifests(datasite)
	if err != nil {
		return nil, err
	}

	hash := syftmsg.HashPrincipal(principal)
	if manifest, ok := manifests[hash]; ok {
		return manifest, nil
	}

	if manifest, ok := manifests["public"]; ok {
		return manifest, nil
	}

	return nil, nil
}
