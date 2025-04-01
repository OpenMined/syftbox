package explorer

import (
	"io"
	"log/slog"
	"maps"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	_ "embed"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/blob"
)

var pathSep = string(filepath.Separator)

//go:embed index.html.tpl
var indexOfTmpl string

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

// Explorer handles browsing and file downloads from a blob service
type Explorer struct {
	svc      *blob.BlobService
	template *template.Template
}

// New creates a new Explorer instance
func New(svc *blob.BlobService) *Explorer {
	funcMap := template.FuncMap{
		"basename": filepath.Base,
	}
	tmpl := template.Must(template.New("index").Funcs(funcMap).Parse(indexOfTmpl))

	return &Explorer{
		svc:      svc,
		template: tmpl,
	}
}

func (e *Explorer) Handler(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("filepath"), "/")

	// todo - you must do permissions check else anyone can pull private content
	// two ways - either get AclService.CanAccess or use DatasiteService.GetView("*")
	contents := e.listContents(path)

	if contents.IsDir {
		e.serveDir(c, path, contents)
	} else {
		e.serveFile(c, path)
	}
}

// List files and folders from the blob index
func (e *Explorer) listContents(prefix string) *directoryContents {
	files := []*blob.BlobInfo{}
	folders := map[string]bool{}
	isDir := false

	datasite := datasiteFromPath(prefix)
	slog.Info("Listing contents", "prefix", prefix, "datasite", datasite)

	// Normalize prefix to end with a slash if not empty
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	// todo - this code is potentially broken it can return `api_data`
	filterPrefix := prefix
	if datasite != "" {
		filterPrefix = datasite + "/public/"
	}

	blobs := e.svc.Index().FilterByPrefix(filterPrefix)

	for _, blob := range blobs {
		if strings.HasPrefix(blob.Key, prefix) {
			relPath := strings.TrimPrefix(blob.Key, prefix)
			if relPath == "" {
				// This is the directory itself, not a file inside it
				continue
			}

			isDir = true // Found something with this prefix, so it's a directory
			parts := strings.SplitN(relPath, "/", 2)

			if len(parts) == 2 {
				// It's a folder
				folders[parts[0]] = true
			} else {
				// It's a file
				files = append(files, blob)
			}
		}
	}

	// Get folder names from the map and sort them
	folderNames := slices.Sorted(maps.Keys(folders))
	return &directoryContents{
		IsDir:   isDir,
		Files:   files,
		Folders: folderNames,
	}
}

// Serve the "Index Of" page
func (e *Explorer) serveDir(c *gin.Context, path string, contents *directoryContents) {
	data := indexData{
		Path:    path,
		Folders: contents.Folders,
		Files:   contents.Files,
	}

	// Generate an HTML response
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := e.template.Execute(c.Writer, data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// Serve a file from S3
func (e *Explorer) serveFile(c *gin.Context, key string) {
	resp, err := e.svc.Client().Download(c.Request.Context(), key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	defer resp.Body.Close()

	// resp.ContentType may not have the correct MIME type
	contentType := e.detectContentType(key)
	c.Header("Content-Type", contentType)

	// Stream response body directly
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
}

func (e *Explorer) detectContentType(key string) string {
	if mimeType := mime.TypeByExtension(filepath.Ext(key)); mimeType != "" {
		return mimeType
	}
	return "application/octet-stream"
}

func datasiteFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 1 {
		return parts[0]
	}
	return ""
}
