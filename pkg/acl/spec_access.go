package acl

import (
	"fmt"

	mapset "github.com/deckarep/golang-set/v2"
)

type Access struct {
	Admin mapset.Set[string] `json:"admin" yaml:"admin"`
	Read  mapset.Set[string] `json:"read"  yaml:"read"`
	Write mapset.Set[string] `json:"write" yaml:"write"`
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
	return NewAccess(empty, empty, []string{Everyone})
}

// PublicReadWriteAccess returns an Access object with read and write access for everyone.
func PublicReadWriteAccess() *Access {
	return NewAccess(empty, []string{Everyone}, empty)
}

// SharedReadAccess returns an Access object with read access for the specified users.
func SharedReadAccess(users ...string) *Access {
	return NewAccess(empty, empty, users)
}

// SharedWriteAccess returns an Access object with write access for the specified users.
func SharedReadWriteAccess(users ...string) *Access {
	return NewAccess(empty, users, empty)
}

func (a *Access) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var m map[string][]string
	if err := unmarshal(&m); err != nil {
		return err
	}

	a.Admin = mapset.NewSet[string]()
	a.Read = mapset.NewSet[string]()
	a.Write = mapset.NewSet[string]()

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

func (a *Access) String() string {
	return fmt.Sprintf("r:%s w:%s a:%s", a.Read.String(), a.Write.String(), a.Admin.String())
}
