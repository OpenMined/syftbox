package explorer

import "github.com/openmined/syftbox/internal/server/blob"

// indexData contains data for the index template
type indexData struct {
	Path        string
	Folders     []string
	Files       []*blob.BlobInfo
	IsSubdomain bool   // Whether this is served via subdomain
	BasePath    string // Base path for links (empty for subdomain, "/datasites" for direct)
	IsRootPage  bool   // Whether this is the root page ("/")
	Scheme      string // Scheme (http or https) for constructing subdomain URLs
	BaseURL     string // Base URL (host) for constructing subdomain URLs
}

// directoryContents holds the result of listing a directory
type directoryContents struct {
	IsDir    bool
	HasIndex bool
	Files    []*blob.BlobInfo
	Folders  []string
}

func (d *directoryContents) EmptyDir() bool {
	return d.IsDir && len(d.Files) == 0 && len(d.Folders) == 0
}
