package explorer

import (
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	_ "embed"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/server/middlewares"
)

//go:embed index.html.tmpl
var indexOfTmpl string

type ExplorerHandler struct {
	blob     blob.Service
	acl      acl.Service
	tplIndex *template.Template
}

// New creates a new Explorer instance
func New(blobSvc blob.Service, aclSvc acl.Service) *ExplorerHandler {
	funcMap := template.FuncMap{
		"basename": filepath.Base,
		"humanizeSize": func(size int64) string {
			return humanize.Bytes(uint64(size))
		},
		"subdomainURL": func(email, baseURL string, scheme string) string {
			hash := datasite.EmailToSubdomainHash(email)
			return scheme + "://" + hash + "." + baseURL
		},
	}

	tplIndex := template.Must(template.New("index").Funcs(funcMap).Parse(indexOfTmpl))

	return &ExplorerHandler{
		blob:     blobSvc,
		acl:      aclSvc,
		tplIndex: tplIndex,
	}
}

func (e *ExplorerHandler) Handler(c *gin.Context) {
	// by default explorer will serve index.html if it exists
	// but we can disable this by passing ?index=0
	serveIndex, err := strconv.ParseBool(c.Query("serveIndex"))
	if err != nil {
		serveIndex = true
	}

	path := strings.TrimPrefix(c.Param("filepath"), "/")
	contents := e.listContents(path)
	if contents.HasIndex && serveIndex {
		e.serveIndex(c, path)
	} else if contents.IsDir || contents.EmptyDir() {
		e.serveDir(c, path, contents)
	} else {
		e.serveFile(c, path)
	}
}

// List files and folders from the blob index
func (e *ExplorerHandler) listContents(prefix string) *directoryContents {
	files := []*blob.BlobInfo{}
	folders := map[string]bool{}
	isDir := strings.HasSuffix(prefix, "/") || prefix == ""

	// Normalize prefix to end with a slash if not empty
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	datasite := datasiteFromPath(prefix)

	var filterPrefix string
	if datasite == "" {
		// root index - show all datasites with ACL files
		filterPrefix = "*/" + aclspec.ACLFileName
	} else {
		// Show content based on the actual path, let ACL system control access
		filterPrefix = prefix
	}

	blobs, err := e.blob.Index().FilterByPrefix(filterPrefix)
	if err != nil {
		slog.Error("Failed to filter blobs by prefix", "error", err)
		return &directoryContents{
			IsDir:    false,
			HasIndex: false,
			Files:    []*blob.BlobInfo{},
			Folders:  []string{},
		}
	}

	var hasIndex bool
	for _, blob := range blobs {
		// check if public readable
		if err := e.acl.CanAccess(
			&acl.User{ID: aclspec.Everyone},
			&acl.File{Path: blob.Key},
			acl.AccessRead,
		); err != nil {
			// don't reveal if the file is private or not
			continue
		}

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
				if strings.HasSuffix(blob.Key, "/index.html") {
					hasIndex = true
				}
			}
		}
	}

	// Get folder names from the map and sort them
	folderNames := slices.Sorted(maps.Keys(folders))
	return &directoryContents{
		IsDir:    isDir,
		HasIndex: hasIndex,
		Files:    files,
		Folders:  folderNames,
	}
}

// Serve the "Index Of" page
func (e *ExplorerHandler) serveDir(c *gin.Context, path string, contents *directoryContents) {
	if path == "" {
		path = "/"
	}
	if path != "" && !strings.HasSuffix(path, "/") {
		path = path + "/"
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Check if this is a subdomain request
	isSubdomain := middlewares.IsSubdomainRequest(c)
	basePath := "/datasites"
	if isSubdomain {
		basePath = ""
	}

	// figure the baseurl only for the root page
	var baseURL string
	var scheme string
	isRootPage := path == "/"

	if isRootPage {
		// Construct base URL from request
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		baseURL = c.Request.Host
	}

	data := indexData{
		Path:        path,
		Folders:     contents.Folders,
		Files:       contents.Files,
		IsSubdomain: isSubdomain,
		BasePath:    basePath,
		IsRootPage:  isRootPage,
		BaseURL:     baseURL,
		Scheme:      scheme,
	}

	// Generate an HTML response
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := e.tplIndex.Execute(c.Writer, data); err != nil {
		api.Serve500HTML(c, fmt.Errorf("failed to execute template: %w", err))
	}
}

func (e *ExplorerHandler) serveIndex(c *gin.Context, path string) {
	e.serveFile(c, filepath.Join(path, "index.html"))
}

// Serve a file from S3
func (e *ExplorerHandler) serveFile(c *gin.Context, key string) {
	_, exists := e.blob.Index().Get(key)
	if !exists {
		api.Serve404HTML(c)
		return
	}

	if err := e.acl.CanAccess(
		&acl.User{ID: aclspec.Everyone},
		&acl.File{Path: key},
		acl.AccessRead,
	); err != nil {
		// don't reveal if the file is private or not
		api.Serve403HTML(c)
		return
	}

	resp, err := e.blob.Backend().GetObject(c.Request.Context(), key)
	if err != nil {
		api.Serve404HTML(c)
		return
	}
	defer resp.Body.Close()

	// resp.ContentType may not have the correct MIME type
	contentType := e.detectContentType(key)
	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)

	// Stream response body directly
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		api.Serve500HTML(c, fmt.Errorf("failed to stream file: %w", err))
		return
	}
}

func (e *ExplorerHandler) detectContentType(key string) string {
	if isTextLike(key) {
		return "text/plain; charset=utf-8"
	} else if mimeType := mime.TypeByExtension(filepath.Ext(key)); mimeType != "" {
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

func isTextLike(key string) bool {
	return strings.HasSuffix(key, ".yaml") ||
		strings.HasSuffix(key, ".yml") ||
		strings.HasSuffix(key, ".toml") ||
		strings.HasSuffix(key, ".md")
}
