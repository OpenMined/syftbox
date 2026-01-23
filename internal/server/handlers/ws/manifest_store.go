package ws

import (
	"sync"

	"github.com/openmined/syftbox/internal/syftmsg"
)

type ManifestStore struct {
	mu        sync.RWMutex
	manifests map[string]map[string]*syftmsg.ACLManifest // datasite -> forHash -> manifest
}

func NewManifestStore() *ManifestStore {
	return &ManifestStore{
		manifests: make(map[string]map[string]*syftmsg.ACLManifest),
	}
}

func (s *ManifestStore) Store(manifest *syftmsg.ACLManifest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.manifests[manifest.Datasite]; !ok {
		s.manifests[manifest.Datasite] = make(map[string]*syftmsg.ACLManifest)
	}
	s.manifests[manifest.Datasite][manifest.ForHash] = manifest
}

func (s *ManifestStore) Get(datasite, forHash string) *syftmsg.ACLManifest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ds, ok := s.manifests[datasite]; ok {
		return ds[forHash]
	}
	return nil
}

func (s *ManifestStore) GetForUser(datasite, user string) *syftmsg.ACLManifest {
	hash := syftmsg.HashPrincipal(user)

	if manifest := s.Get(datasite, hash); manifest != nil {
		return manifest
	}

	return s.Get(datasite, "public")
}

func (s *ManifestStore) GetAllForUser(user string) []*syftmsg.ACLManifest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash := syftmsg.HashPrincipal(user)
	var results []*syftmsg.ACLManifest

	for _, ds := range s.manifests {
		if manifest, ok := ds[hash]; ok {
			results = append(results, manifest)
		} else if manifest, ok := ds["public"]; ok {
			results = append(results, manifest)
		}
	}

	return results
}

func (s *ManifestStore) GetAllManifestsForDatasite(datasite string) map[string]*syftmsg.ACLManifest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ds, ok := s.manifests[datasite]; ok {
		result := make(map[string]*syftmsg.ACLManifest)
		for k, v := range ds {
			result[k] = v
		}
		return result
	}
	return nil
}
