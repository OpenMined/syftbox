package aclspec

import (
	mapset "github.com/deckarep/golang-set/v2"
	"gopkg.in/yaml.v3"
)

var empty = []string{}

type Access struct {
	Admin mapset.Set[string] `yaml:"admin"`
	Read  mapset.Set[string] `yaml:"read"`
	Write mapset.Set[string] `yaml:"write"`
}

// NewAccess creates a new Access object with the specified admin, write, and read users.
func NewAccess(admin []string, write []string, read []string) *Access {
	return &Access{
		Admin: mapset.NewSet(admin...),
		Write: mapset.NewSet(write...),
		Read:  mapset.NewSet(read...),
	}
}

// PrivateAccess returns an Access object with no users.
func PrivateAccess() *Access {
	return NewAccess(empty, empty, empty)
}

// PublicReadAccess returns an Access object with read access for everyone.
func PublicReadAccess() *Access {
	return NewAccess(empty, empty, []string{TokenEveryone})
}

// PublicReadWriteAccess returns an Access object with read and write access for everyone.
func PublicReadWriteAccess() *Access {
	return NewAccess(empty, []string{TokenEveryone}, empty)
}

// SharedReadAccess returns an Access object with read access for the specified users.
func SharedReadAccess(users ...string) *Access {
	return NewAccess(empty, empty, users)
}

// SharedWriteAccess returns an Access object with write access for the specified users.
func SharedReadWriteAccess(users ...string) *Access {
	return NewAccess(empty, users, empty)
}

func (a *Access) UnmarshalYAML(value *yaml.Node) error {
	// Create a map to decode the YAML into
	var m map[string][]string
	if err := value.Decode(&m); err != nil {
		return err
	}

	// Initialize sets
	a.Admin = mapset.NewSet[string]()
	a.Read = mapset.NewSet[string]()
	a.Write = mapset.NewSet[string]()

	// Add values to sets
	if admin, ok := m["admin"]; ok {
		a.Admin.Append(admin...)
	}
	if read, ok := m["read"]; ok {
		a.Read.Append(read...)
	}
	if write, ok := m["write"]; ok {
		a.Write.Append(write...)
	}

	return nil
}

func (a Access) MarshalYAML() (interface{}, error) {
	// Create map to be marshaled
	m := make(map[string][]string)
	if a.Admin != nil {
		m["admin"] = a.Admin.ToSlice()
	}
	if a.Read != nil {
		m["read"] = a.Read.ToSlice()
	}
	if a.Write != nil {
		m["write"] = a.Write.ToSlice()
	}
	return m, nil
}
