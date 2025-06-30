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
	"strings"
	"text/template"

	_ "embed"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/server/middlewares"
)

//go:embed index.html.tmpl
var indexOfTmpl string

//go:embed not_found.html.tmpl
var notFoundOfTmpl string

type ExplorerHandler struct {
	blob     *blob.BlobService
	acl      *acl.ACLService
	tplIndex *template.Template
	tpl404   *template.Template
}

// New creates a new Explorer instance
func New(svc *blob.BlobService, acl *acl.ACLService) *ExplorerHandler {
	funcMap := template.FuncMap{
		"basename": filepath.Base,
		"humanizeSize": func(size int64) string {
			return humanize.Bytes(uint64(size))
		},
	}

	tplIndex := template.Must(template.New("index").Funcs(funcMap).Parse(indexOfTmpl))
	tpl404 := template.Must(template.New("notfound").Funcs(funcMap).Parse(notFoundOfTmpl))

	return &ExplorerHandler{
		blob:     svc,
		acl:      acl,
		tplIndex: tplIndex,
		tpl404:   tpl404,
	}
}

func (e *ExplorerHandler) Handler(c *gin.Context) {
	// For subdomain requests that were rewritten, use the full request path
	// Otherwise use the filepath parameter from the route
	path := ""
	if middlewares.IsSubdomainRequest(c) {
		// Use the rewritten path, removing the leading /datasites/
		path = strings.TrimPrefix(c.Request.URL.Path, "/datasites/")
	} else {
		path = strings.TrimPrefix(c.Param("filepath"), "/")
	}

	// For subdomain requests, we need to handle the path differently
	// The middleware has already rewritten the path to /datasites/{email}/public/*
	// but we need to be aware of this for proper display and navigation
	if middlewares.IsSubdomainRequest(c) {
		// The path already includes the datasite prefix from the middleware rewrite
		// We need to store this information for proper link generation in templates
		c.Set("subdomain_mode", true)
		if email, exists := middlewares.GetSubdomainEmail(c); exists {
			c.Set("datasite_email", email)
		}
	}

	contents := e.listContents(path)
	if contents.IsDir || contents.EmptyDir() {
		// Check if index.html exists in this directory
		indexPath := path
		if !strings.HasSuffix(indexPath, "/") {
			indexPath += "/"
		}
		indexPath += "index.html"

		// Try to find index.html in the directory
		if _, exists := e.blob.Index().Get(indexPath); exists {
			// Check if user has access to the index.html
			if err := e.acl.CanAccess(
				&acl.User{ID: aclspec.Everyone},
				&acl.File{Path: indexPath},
				acl.AccessRead,
			); err == nil {
				// Serve index.html instead of directory listing
				e.serveFile(c, indexPath)
				return
			}
		}
		// Show directory listing
		e.serveDir(c, path, contents)
	} else {
		e.serveFile(c, path)
	}
}

// List files and folders from the blob index
func (e *ExplorerHandler) listContents(prefix string) *directoryContents {
	files := []*blob.BlobInfo{}
	folders := map[string]bool{}
	isDir := false

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
			IsDir:   false,
			Files:   []*blob.BlobInfo{},
			Folders: []string{},
		}
	}

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

	data := indexData{
		Path:        path,
		Folders:     contents.Folders,
		Files:       contents.Files,
		IsSubdomain: isSubdomain,
		BasePath:    basePath,
	}

	// Generate an HTML response
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := e.tplIndex.Execute(c.Writer, data); err != nil {
		api.AbortWithError(c, http.StatusInternalServerError, api.CodeInternalError, fmt.Errorf("failed to execute template: %w", err))
	}
}

// Serve a file from S3
func (e *ExplorerHandler) serveFile(c *gin.Context, key string) {
	if err := e.acl.CanAccess(
		&acl.User{ID: aclspec.Everyone},
		&acl.File{Path: key},
		acl.AccessRead,
	); err != nil {
		// don't reveal if the file is private or not
		e.serve404(c, key)
		return
	}

	resp, err := e.blob.Backend().GetObject(c.Request.Context(), key)
	if err != nil {
		e.serve404(c, key)
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
		api.AbortWithError(c, http.StatusInternalServerError, api.CodeInternalError, fmt.Errorf("failed to stream file: %w", err))
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

func (e *ExplorerHandler) serve404(c *gin.Context, key string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusNotFound)
	// Even if template execution fails, we've already set 404 status which is what we want
	if err := e.tpl404.Execute(c.Writer, map[string]any{"Key": key}); err != nil {
		// Log the error but don't try to change status since headers are already sent
		slog.Error("Failed to execute 404 template", "error", err, "key", key)
	}
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
