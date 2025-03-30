package aclspec

type Limits struct {
	MaxFileSize   int64  `yaml:"maxFileSize,omitempty"`
	MaxFiles      uint32 `yaml:"maxFiles,omitempty"`
	AllowDirs     bool   `yaml:"allowDirs,omitempty"`
	AllowSymlinks bool   `yaml:"allowSymlinks,omitempty"`
}

func DefaultLimits() *Limits {
	return &Limits{
		MaxFiles:      0,
		MaxFileSize:   0,
		AllowDirs:     true,
		AllowSymlinks: false,
	}
}
