package handlers

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

type PublicationHandler struct {
	datasiteMgr *datasitemgr.DatasiteManager
}

func NewPublicationHandler(datasiteMgr *datasitemgr.DatasiteManager) *PublicationHandler {
	return &PublicationHandler{datasiteMgr: datasiteMgr}
}

// List publications (ACL files) for the local datasite.
func (h *PublicationHandler) List(c *gin.Context) {
	ds := h.datasiteMgr.GetPrimaryDatasite()
	if ds == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrCodeDatasiteNotReady, errors.New("no active datasite"))
		return
	}

	root := ds.GetWorkspace().UserDir
	base := ds.GetWorkspace().DatasitesDir

	var out []PublicationEntry
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !aclspec.IsACLFile(path) {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			rel = path
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		out = append(out, PublicationEntry{
			Path:    filepath.ToSlash(rel),
			Content: string(content),
		})
		return nil
	})

	c.JSON(http.StatusOK, PublicationsResponse{Files: out})
}
