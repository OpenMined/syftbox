package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

const (
	ErrCodeListWorkspaceItemsFailed = "ERR_LIST_WORKSPACE_ITEMS_FAILED"
)

type WorkspaceHandler struct {
	mgr *datasitemgr.DatasiteManger
}

func NewWorkspaceHandler(mgr *datasitemgr.DatasiteManger) *WorkspaceHandler {
	return &WorkspaceHandler{
		mgr: mgr,
	}
}

// @Summary		Get workspace items
// @Description	Get files and folders at a specified path
// @Tags			Files and Folders
// @Produce		json
// @Param			path	query		string	false	"Path to the directory (default is root)"
// @Param			depth	query		integer	false	"Maximum depth for retrieving children (0 = no children, 1 = immediate children only, etc.)"	minimum(0)	default(1)
// @Success		200		{object}	WorkspaceItemsResponse
// @Failure		500		{object}	ControlPlaneError
// @Failure		503		{object}	ControlPlaneError
// @Router			/v1/workspace/items [get]
func (h *WorkspaceHandler) GetItems(c *gin.Context) {
	var req WorkspaceItemsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     err.Error(),
		})
		return
	}

	ds, err := h.mgr.Get()
	if err != nil {
		c.PureJSON(http.StatusServiceUnavailable, &ControlPlaneError{
			ErrorCode: ErrCodeDatasiteNotReady,
			Error:     err.Error(),
		})
		return
	}

	// Get the workspace
	ws := ds.GetWorkspace()

	// Resolve the path
	absPath := filepath.Join(ws.Root, req.Path)

	// List items at the path
	items, err := h.listItems(absPath, ws.Root, req.Depth)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeListWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	c.PureJSON(http.StatusOK, &WorkspaceItemsResponse{
		Items: items,
	})
}

func (h *WorkspaceHandler) listItems(path string, rootPath string, depth int) ([]WorkspaceItem, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var items []WorkspaceItem
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		absPath := filepath.Join(path, entry.Name())
		relPath, err := filepath.Rel(rootPath, absPath)
		if err != nil {
			continue
		}
		// Convert to forward slashes for consistency
		relPath = filepath.Join("/", filepath.ToSlash(relPath))

		item := WorkspaceItem{
			Id:          relPath, // Using the relative path as the unique identifier
			Name:        entry.Name(),
			Type:        "file",
			Path:        relPath,
			CreatedAt:   info.ModTime(), // Using ModTime as CreatedAt since Go doesn't provide creation time
			ModifiedAt:  info.ModTime(),
			Size:        info.Size(),
			SyncStatus:  SyncStatusHidden, // TODO: Replace with actual sync status
			Permissions: []Permission{},   // TODO: Replace with actual permissions
			Children:    []WorkspaceItem{},
		}

		if entry.IsDir() {
			item.Type = "folder"
			if depth > 0 {
				children, err := h.listItems(absPath, rootPath, depth-1)
				if err != nil {
					continue
				}
				item.Children = children
			}
		}

		items = append(items, item)
	}

	return items, nil
}
