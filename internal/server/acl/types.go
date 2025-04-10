package acl

import "path/filepath"

var pathSep = string(filepath.Separator)

type User struct {
	ID      string
	IsOwner bool
}

type File struct {
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
}
