package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

const (
	ErrCodeListWorkspaceItemsFailed  = "ERR_LIST_WORKSPACE_ITEMS_FAILED"
	ErrCodeCreateWorkspaceItemFailed = "ERR_CREATE_WORKSPACE_ITEM_FAILED"
	ErrCodeDeleteWorkspaceItemFailed = "ERR_DELETE_WORKSPACE_ITEM_FAILED"
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

// @Summary		Create workspace item
// @Description	Create a new file or folder in the workspace
// @Tags			Files and Folders
// @Accept			json
// @Produce		json
// @Param			request	body		WorkspaceItemCreateRequest	true	"Request body"
// @Success		201		{object}	WorkspaceItemCreateResponse
// @Failure		400		{object}	ControlPlaneError
// @Failure		500		{object}	ControlPlaneError
// @Failure		503		{object}	ControlPlaneError
// @Router			/v1/workspace/items [post]
func (h *WorkspaceHandler) CreateItem(c *gin.Context) {
	var req WorkspaceItemCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     err.Error(),
		})
		return
	}

	// Get the datasite
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

	// Make sure req.Path is a absolute path
	if !filepath.IsAbs(req.Path) {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "path must be an absolute path and start with /",
		})
		return
	}

	// Resolve the path
	absPath := filepath.Join(ws.Root, req.Path)

	// Create the item
	var itemInfo os.FileInfo

	dirToCreate := absPath
	if req.Type == "file" {
		dirToCreate = filepath.Dir(absPath)
	}

	// Create the directory
	if err := os.MkdirAll(dirToCreate, 0755); err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCreateWorkspaceItemFailed,
			Error:     err.Error(),
		})
		return
	}
	info, err := os.Stat(dirToCreate)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCreateWorkspaceItemFailed,
			Error:     err.Error(),
		})
		return
	}
	itemInfo = info

	if req.Type == "file" {
		// Create a file
		f, err := os.Create(absPath)
		if err != nil {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeCreateWorkspaceItemFailed,
				Error:     err.Error(),
			})
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeCreateWorkspaceItemFailed,
				Error:     err.Error(),
			})
			return
		}
		itemInfo = info
	}

	// Get relative path for response
	relPath, err := filepath.Rel(ws.Root, absPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCreateWorkspaceItemFailed,
			Error:     err.Error(),
		})
		return
	}
	relPath = filepath.Join("/", filepath.ToSlash(relPath))

	// Create response item
	item := WorkspaceItem{
		Id:          relPath,
		Name:        filepath.Base(absPath),
		Type:        WorkspaceItemType(req.Type),
		Path:        relPath,
		CreatedAt:   itemInfo.ModTime(),
		ModifiedAt:  itemInfo.ModTime(),
		Size:        itemInfo.Size(),
		SyncStatus:  SyncStatusHidden, // TODO: Replace with actual sync status
		Permissions: []Permission{},   // TODO: Replace with actual permissions
		Children:    []WorkspaceItem{},
	}

	c.PureJSON(http.StatusCreated, &WorkspaceItemCreateResponse{
		Item: item,
	})
}

// @Summary		Delete workspace items
// @Description	Delete multiple files or folders. The operation is similar to the Unix `rm -rf` command.
// @Description	- If the path is a file, the file will be deleted.
// @Description	- If the path is a folder, all its contents will also be deleted.
// @Description	- If the path is a symlink, the symlink will be deleted without deleting the target.
// @Description	- If the path does not exist, the operation will be a no-op.
// @Tags			Files and Folders
// @Accept			json
// @Param			request	body		WorkspaceItemDeleteRequest	true	"Request body"
// @Success		204		{object}	nil
// @Failure		400		{object}	ControlPlaneError
// @Failure		500		{object}	ControlPlaneError
// @Failure		503		{object}	ControlPlaneError
// @Router			/v1/workspace/items [delete]
func (h *WorkspaceHandler) DeleteItems(c *gin.Context) {
	var req WorkspaceItemDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     err.Error(),
		})
		return
	}

	// Get the datasite
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

	// Process each path
	for _, path := range req.Paths {
		// Make sure path is an absolute path
		if !filepath.IsAbs(path) {
			c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
				ErrorCode: ErrCodeBadRequest,
				Error:     "all paths must be absolute paths and start with /",
			})
			return
		}

		// Resolve the path
		absPath := filepath.Join(ws.Root, path)

		// Check if the path exists and get info
		fileInfo, err := os.Lstat(absPath)
		if err != nil {
			// If the path doesn't exist, skip it (no-op)
			if os.IsNotExist(err) {
				continue
			}

			// For other errors, return an error
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeDeleteWorkspaceItemFailed,
				Error:     err.Error(),
			})
			return
		}

		// Handle different types of paths
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			// For symlinks, only delete the link itself
			err = os.Remove(absPath)
		} else if fileInfo.IsDir() {
			// For directories, use RemoveAll to delete recursively
			err = os.RemoveAll(absPath)
		} else {
			// For regular files, use Remove
			err = os.Remove(absPath)
		}
		if err != nil {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeDeleteWorkspaceItemFailed,
				Error:     err.Error(),
			})
			return
		}
	}

	// Return 204 No Content
	c.Status(http.StatusNoContent)
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
