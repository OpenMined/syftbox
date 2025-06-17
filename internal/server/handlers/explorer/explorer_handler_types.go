package explorer

import "github.com/openmined/syftbox/internal/server/blob"

// indexData contains data for the index template
type indexData struct {
	Path    string
	Folders []string
	Files   []*blob.BlobInfo
}

// directoryContents holds the result of listing a directory
type directoryContents struct {
	IsDir   bool
	Files   []*blob.BlobInfo
	Folders []string
}

func (d *directoryContents) IsEmpty() bool {
	return len(d.Files) == 0 && len(d.Folders) == 0
}
