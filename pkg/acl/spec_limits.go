package acl

import "fmt"

type Limit struct {
	MaxFileSize   uint64 `json:"maxFileSize"   yaml:"maxFileSize"`
	MaxFiles      uint32 `json:"maxFiles"      yaml:"maxFiles"`
	AllowDirs     bool   `json:"allowDirs"     yaml:"allowDirs"`
	AllowSymlinks bool   `json:"allowSymlinks" yaml:"allowSymlinks"`
}

// NewLimits returns a new Limit object with the specified values.
func NewLimits(maxFiles uint32, maxFileSize uint64, allowDirs, allowSymlinks bool) *Limit {
	return &Limit{
		MaxFiles:      maxFiles,
		MaxFileSize:   maxFileSize,
		AllowDirs:     allowDirs,
		AllowSymlinks: allowSymlinks,
	}
}

// DefaultLimits returns a new Limit object with default values.
// MaxFiles and MaxFileSize are set to 0, allowing unlimited files and file sizes.
// AllowDirs is set to true, allowing directories.
// AllowSymlinks is set to false, disallowing symlinks.
func DefaultLimits() *Limit {
	return &Limit{
		MaxFiles:      0,
		MaxFileSize:   0,
		AllowDirs:     true,
		AllowSymlinks: false,
	}
}

func (l *Limit) String() string {
	return fmt.Sprintf("maxFiles:%d maxFileSize:%d allowDirs:%t allowSymlinks:%t", l.MaxFiles, l.MaxFileSize, l.AllowDirs, l.AllowSymlinks)
}
