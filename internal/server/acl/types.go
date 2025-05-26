package acl

import (
	"path/filepath"
	"strings"
)

const PathSep = "/"

type User struct {
	ID string
}

type File struct {
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
}

func CleanACLPath(path string) string {
	return strings.TrimLeft(filepath.Clean(path), PathSep)
}
