package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

const (
	ErrCodeListWorkspaceItemsFailed  = "ERR_LIST_WORKSPACE_ITEMS_FAILED"
	ErrCodeCreateWorkspaceItemFailed = "ERR_CREATE_WORKSPACE_ITEM_FAILED"
	ErrCodeDeleteWorkspaceItemFailed = "ERR_DELETE_WORKSPACE_ITEM_FAILED"
	ErrCodeMoveWorkspaceItemsFailed  = "ERR_MOVE_WORKSPACE_ITEMS_FAILED"
	ErrCodeCopyWorkspaceItemsFailed  = "ERR_COPY_WORKSPACE_ITEMS_FAILED"
	ErrCodeGetWorkspaceContentFailed = "ERR_GET_WORKSPACE_CONTENT_FAILED"
)

type WorkspaceHandler struct {
	mgr *datasitemgr.DatasiteManager
}

func NewWorkspaceHandler(mgr *datasitemgr.DatasiteManager) *WorkspaceHandler {
	return &WorkspaceHandler{
		mgr: mgr,
	}
}

// GetItems gets workspace at a specified path
//
//	@Summary		Get workspace items
//	@Description	Get files and folders at a specified path
//	@Tags			Workspace
//	@Produce		json
//	@Param			path	query		string	false	"Path to the directory (default is root)"
//	@Param			depth	query		integer	false	"Maximum depth for retrieving children (0 = no children, 1 = immediate children only, etc.)"	minimum(0)	default(1)
//	@Success		200		{object}	WorkspaceItemsResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/workspace/items [get]
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
	absPath := req.Path
	if !strings.HasPrefix(absPath, ws.Root) {
		absPath = filepath.Join(ws.Root, absPath)
	}

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

// Create workspace item
//
//	@Summary		Create workspace item
//	@Description	Create a new file or folder in the workspace
//	@Tags			Workspace
//	@Accept			json
//	@Produce		json
//	@Param			request	body		WorkspaceItemCreateRequest	true	"Request body"
//	@Success		201		{object}	WorkspaceItemCreateResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/workspace/items [post]
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
	if !strings.HasPrefix(req.Path, "/") {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "path must be an absolute path and start with /",
		})
		return
	}

	// Resolve the path
	absPath := filepath.Join(ws.Root, req.Path)

	// Check if the item already exists
	if existingInfo, err := os.Stat(absPath); err == nil {
		if req.Overwrite {
			// If overwrite is true, remove the existing file/directory
			if existingInfo.IsDir() {
				err = os.RemoveAll(absPath)
			} else {
				err = os.Remove(absPath)
			}

			if err != nil {
				c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
					ErrorCode: ErrCodeCreateWorkspaceItemFailed,
					Error:     "failed to remove existing item: " + err.Error(),
				})
				return
			}
		} else {
			// If overwrite is false, return conflict error with the existing item
			relPath, _ := filepath.Rel(ws.Root, absPath)
			relPath = filepath.Join("/", filepath.ToSlash(relPath))

			itemType := WorkspaceItemTypeFile
			if existingInfo.IsDir() {
				itemType = WorkspaceItemTypeFolder
			}

			existingItem := WorkspaceItem{
				Id:           relPath,
				Name:         filepath.Base(absPath),
				Type:         itemType,
				Path:         relPath,
				AbsolutePath: absPath,
				CreatedAt:    existingInfo.ModTime(),
				ModifiedAt:   existingInfo.ModTime(),
				Size:         existingInfo.Size(),
				SyncStatus:   SyncStatusHidden,
				Permissions:  []Permission{},
				Children:     []WorkspaceItem{},
			}

			c.PureJSON(http.StatusConflict, &WorkspaceConflictError{
				ErrorCode:    ErrCodeCreateWorkspaceItemFailed,
				Error:        "item already exists: " + req.Path,
				ExistingItem: existingItem,
			})
			return
		}
	} else if !os.IsNotExist(err) {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCreateWorkspaceItemFailed,
			Error:     err.Error(),
		})
		return
	}

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
		Id:           relPath,
		Name:         filepath.Base(absPath),
		Type:         WorkspaceItemType(req.Type),
		Path:         relPath,
		AbsolutePath: absPath,
		CreatedAt:    itemInfo.ModTime(),
		ModifiedAt:   itemInfo.ModTime(),
		Size:         itemInfo.Size(),
		SyncStatus:   SyncStatusHidden, // TODO: Replace with actual sync status
		Permissions:  []Permission{},   // TODO: Replace with actual permissions
		Children:     []WorkspaceItem{},
	}

	c.PureJSON(http.StatusCreated, &WorkspaceItemCreateResponse{
		Item: item,
	})
}

// Delete workspace items
//
//	@Summary		Delete workspace items
//	@Description	Delete multiple files or folders. The operation is similar to the Unix `rm -rf` command.
//	@Description	- If the path is a file, the file will be deleted.
//	@Description	- If the path is a folder, all its contents will also be deleted.
//	@Description	- If the path is a symlink, the symlink will be deleted without deleting the target.
//	@Description	- If the path does not exist, the operation will be a no-op.
//	@Tags			Workspace
//	@Accept			json
//	@Param			request	body		WorkspaceItemDeleteRequest	true	"Request body"
//	@Success		204		{object}	nil
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/workspace/items [delete]
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
		if !strings.HasPrefix(path, "/") {
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
		if fileInfo.IsDir() {
			// For directories, use RemoveAll to delete recursively
			err = os.RemoveAll(absPath)
		} else {
			// For regular files and symlinks, use Remove
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

// MoveItems moves an item to a new location. Can also be used for renaming an item.
//
//	@Summary		Move item
//	@Description	Move an item to a new location. Can also be used for renaming an item.
//	@Tags			Workspace
//	@Accept			json
//	@Produce		json
//	@Param			request	body		WorkspaceItemMoveRequest	true	"Request body"
//	@Success		200		{object}	WorkspaceItemMoveResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		404		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Router			/v1/workspace/items/move [post]
func (h *WorkspaceHandler) MoveItems(c *gin.Context) {
	var req WorkspaceItemMoveRequest
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

	// Validate source path
	if !strings.HasPrefix(req.SourcePath, "/") {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "source path must be an absolute path and start with /",
		})
		return
	}

	// Validate destination path
	if !strings.HasPrefix(req.NewPath, "/") {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "destination path must be an absolute path and start with /",
		})
		return
	}

	// Resolve the source path
	absSourcePath := filepath.Join(ws.Root, req.SourcePath)

	// Resolve the destination path
	absNewPath := filepath.Join(ws.Root, req.NewPath)
	newDir := filepath.Dir(absNewPath)

	// Check if the source exists
	_, err = os.Stat(absSourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.PureJSON(http.StatusNotFound, &ControlPlaneError{
				ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
				Error:     "source file or directory does not exist",
			})
			return
		}
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Check if the destination parent directory exists
	newDirInfo, err := os.Stat(newDir)
	if err != nil {
		if os.IsNotExist(err) {
			c.PureJSON(http.StatusNotFound, &ControlPlaneError{
				ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
				Error:     "destination parent directory does not exist",
			})
			return
		}
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Make sure destination parent is a directory
	if !newDirInfo.IsDir() {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "destination's parent is not a directory",
		})
		return
	}

	// Check if the destination already exists
	if newDirInfo, err := os.Stat(absNewPath); err == nil {
		if req.Overwrite {
			// If overwrite is true, remove the existing file/directory

			if newDirInfo.IsDir() {
				err = os.RemoveAll(absNewPath)
			} else {
				err = os.Remove(absNewPath)
			}

			if err != nil {
				c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
					ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
					Error:     "failed to remove existing item: " + err.Error(),
				})
				return
			}
		} else {
			// If overwrite is false, return conflict error with the existing item
			relPath, _ := filepath.Rel(ws.Root, absNewPath)
			relPath = filepath.Join("/", filepath.ToSlash(relPath))

			itemType := WorkspaceItemTypeFile
			if newDirInfo.IsDir() {
				itemType = WorkspaceItemTypeFolder
			}

			existingItem := WorkspaceItem{
				Id:          relPath,
				Name:        filepath.Base(absNewPath),
				Type:        itemType,
				Path:        relPath,
				CreatedAt:   newDirInfo.ModTime(),
				ModifiedAt:  newDirInfo.ModTime(),
				Size:        newDirInfo.Size(),
				SyncStatus:  SyncStatusHidden,
				Permissions: []Permission{},
				Children:    []WorkspaceItem{},
			}

			c.PureJSON(http.StatusConflict, &WorkspaceConflictError{
				ErrorCode:    ErrCodeMoveWorkspaceItemsFailed,
				Error:        "destination already exists: " + req.NewPath,
				ExistingItem: existingItem,
			})
			return
		}
	} else if !os.IsNotExist(err) {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Move the file or directory
	err = os.Rename(absSourcePath, absNewPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Get updated file info
	updatedInfo, err := os.Stat(absNewPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Get relative path for response
	relNewPath, err := filepath.Rel(ws.Root, absNewPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeMoveWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}
	relNewPath = filepath.Join("/", filepath.ToSlash(relNewPath))

	// Create response item
	itemType := WorkspaceItemTypeFile
	if updatedInfo.IsDir() {
		itemType = WorkspaceItemTypeFolder
	}

	item := WorkspaceItem{
		Id:           relNewPath,
		Name:         filepath.Base(absNewPath),
		Type:         itemType,
		Path:         relNewPath,
		AbsolutePath: absNewPath,
		CreatedAt:    updatedInfo.ModTime(),
		ModifiedAt:   updatedInfo.ModTime(),
		Size:         updatedInfo.Size(),
		SyncStatus:   SyncStatusHidden, // TODO: Replace with actual sync status
		Permissions:  []Permission{},   // TODO: Replace with actual permissions
		Children:     []WorkspaceItem{},
	}

	c.PureJSON(http.StatusOK, &WorkspaceItemMoveResponse{
		Item: item,
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
			Id:           relPath, // Using the relative path as the unique identifier
			Name:         entry.Name(),
			Type:         "file",
			Path:         relPath,
			AbsolutePath: absPath,
			CreatedAt:    info.ModTime(), // Using ModTime as CreatedAt since Go doesn't provide creation time
			ModifiedAt:   info.ModTime(),
			Size:         info.Size(),
			SyncStatus:   SyncStatusHidden, // TODO: Replace with actual sync status
			Permissions:  []Permission{},   // TODO: Replace with actual permissions
			Children:     []WorkspaceItem{},
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

// Recursively copy a directory and its contents
func copyDir(src, dst string) error {
	// Create the destination directory
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	// Read the source directory
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	// Process each entry
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectories
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy files
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// Copy a single file
func copyFile(src, dst string) error {
	// Open the source file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Create the destination file
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy the contents
	_, err = dstFile.ReadFrom(srcFile)
	return err
}

// CopyItems copies a file or folder to a new location. Can also be used for renaming a file or folder.
//
//	@Summary		Copy a file or folder
//	@Description	Create a copy of a file or folder
//	@Tags			Workspace
//	@Accept			json
//	@Produce		json
//	@Param			request	body		WorkspaceItemCopyRequest	true	"Request body"
//	@Success		200		{object}	WorkspaceItemCopyResponse
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		404		{object}	ControlPlaneError
//	@Failure		409		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Router			/v1/workspace/items/copy [post]
func (h *WorkspaceHandler) CopyItems(c *gin.Context) {
	var req WorkspaceItemCopyRequest
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

	// Validate source path
	if !strings.HasPrefix(req.SourcePath, "/") {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "source path must be an absolute path and start with /",
		})
		return
	}

	// Validate destination path
	if !strings.HasPrefix(req.NewPath, "/") {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "destination path must be an absolute path and start with /",
		})
		return
	}

	// Resolve the source path
	absSourcePath := filepath.Join(ws.Root, req.SourcePath)

	// Resolve the destination path
	absNewPath := filepath.Join(ws.Root, req.NewPath)
	newDir := filepath.Dir(absNewPath)

	// Check if the source exists
	srcInfo, err := os.Stat(absSourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.PureJSON(http.StatusNotFound, &ControlPlaneError{
				ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
				Error:     "source file or directory does not exist",
			})
			return
		}
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Check if the destination parent directory exists
	newDirInfo, err := os.Stat(newDir)
	if err != nil {
		if os.IsNotExist(err) {
			c.PureJSON(http.StatusNotFound, &ControlPlaneError{
				ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
				Error:     "destination parent directory does not exist",
			})
			return
		}
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Make sure destination parent is a directory
	if !newDirInfo.IsDir() {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "destination's parent is not a directory",
		})
		return
	}

	// Check if the destination already exists
	if existingInfo, err := os.Stat(absNewPath); err == nil {
		if req.Overwrite {
			// If overwrite is true, remove the existing file/directory
			if err := os.RemoveAll(absNewPath); err != nil {
				c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
					ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
					Error:     "failed to remove existing item: " + err.Error(),
				})
				return
			}
		} else {
			// If overwrite is false, return conflict error with the existing item
			relPath, _ := filepath.Rel(ws.Root, absNewPath)
			relPath = filepath.Join("/", filepath.ToSlash(relPath))

			itemType := WorkspaceItemTypeFile
			if existingInfo.IsDir() {
				itemType = WorkspaceItemTypeFolder
			}

			existingItem := WorkspaceItem{
				Id:           relPath,
				Name:         filepath.Base(absNewPath),
				Type:         itemType,
				Path:         relPath,
				AbsolutePath: absNewPath,
				CreatedAt:    existingInfo.ModTime(),
				ModifiedAt:   existingInfo.ModTime(),
				Size:         existingInfo.Size(),
				SyncStatus:   SyncStatusHidden,
				Permissions:  []Permission{},
				Children:     []WorkspaceItem{},
			}

			c.PureJSON(http.StatusConflict, &WorkspaceConflictError{
				ErrorCode:    ErrCodeCopyWorkspaceItemsFailed,
				Error:        "destination already exists: " + req.NewPath,
				ExistingItem: existingItem,
			})
			return
		}
	} else if !os.IsNotExist(err) {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Copy the file or directory
	if srcInfo.IsDir() {
		// Copy directory recursively
		if err := copyDir(absSourcePath, absNewPath); err != nil {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
				Error:     "failed to copy directory: " + err.Error(),
			})
			return
		}
	} else {
		// Copy file
		if err := copyFile(absSourcePath, absNewPath); err != nil {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
				Error:     "failed to copy file: " + err.Error(),
			})
			return
		}
	}

	// Get updated file info
	updatedInfo, err := os.Stat(absNewPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}

	// Get relative path for response
	relNewPath, err := filepath.Rel(ws.Root, absNewPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeCopyWorkspaceItemsFailed,
			Error:     err.Error(),
		})
		return
	}
	relNewPath = filepath.Join("/", filepath.ToSlash(relNewPath))

	// Create response item
	itemType := WorkspaceItemTypeFile
	if updatedInfo.IsDir() {
		itemType = WorkspaceItemTypeFolder
	}

	item := WorkspaceItem{
		Id:           relNewPath,
		Name:         filepath.Base(absNewPath),
		Type:         itemType,
		Path:         relNewPath,
		AbsolutePath: absNewPath,
		CreatedAt:    updatedInfo.ModTime(),
		ModifiedAt:   updatedInfo.ModTime(),
		Size:         updatedInfo.Size(),
		SyncStatus:   SyncStatusHidden, // TODO: Replace with actual sync status
		Permissions:  []Permission{},   // TODO: Replace with actual permissions
		Children:     []WorkspaceItem{},
	}

	c.PureJSON(http.StatusOK, &WorkspaceItemCopyResponse{
		Item: item,
	})
}

// getContentType returns the MIME type based on file extension
func getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt":
		return "text/plain"
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".md":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".py":
		return "text/x-python"
	case ".go":
		return "text/x-go"
	case ".rs":
		return "text/x-rust"
	case ".java":
		return "text/x-java"
	case ".c":
		return "text/x-c"
	case ".cpp", ".cc", ".cxx":
		return "text/x-c++"
	case ".h", ".hpp":
		return "text/x-c-header"
	case ".sh":
		return "text/x-shellscript"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".toml":
		return "text/toml"
	case ".ini":
		return "text/plain"
	case ".log":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// Get file content
//
//	@Summary		Get file content
//	@Description	Get the content of a file at the specified path. Supports range requests for efficient streaming of large files.
//	@Tags			Workspace
//	@Produce		text/plain
//	@Produce		application/octet-stream
//	@Produce		*/*
//	@Param			path	query		string	true	"Path to the file"
//	@Success		200		{file}		file	"File content"
//	@Success		206		{file}		file	"Partial file content for range requests"
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		404		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/workspace/content [get]
func (h *WorkspaceHandler) GetContent(c *gin.Context) {
	var req WorkspaceContentRequest
	if err := c.ShouldBindQuery(&req); err != nil {
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

	// Make sure req.Path is an absolute path
	if !strings.HasPrefix(req.Path, "/") {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "path must be an absolute path and start with /",
		})
		return
	}

	// Resolve the path
	absPath := filepath.Join(ws.Root, req.Path)

	// Check if the file exists
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.PureJSON(http.StatusNotFound, &ControlPlaneError{
				ErrorCode: ErrCodeGetWorkspaceContentFailed,
				Error:     "file not found",
			})
			return
		}
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeGetWorkspaceContentFailed,
			Error:     err.Error(),
		})
		return
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "path points to a directory, not a file",
		})
		return
	}

	// Get content type based on file extension
	contentType := getContentType(absPath)

	// Set appropriate headers
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filepath.Base(absPath)))
	c.Header("Accept-Ranges", "bytes")

	// Serve the file with range request support
	c.File(absPath)
}

// UpdateContent handles PUT requests to update file content
//
//	@Summary		Update file content
//	@Description	Update the content of a file at the specified path. Supports overwrite, append, and prepend modes. Can create the file if it doesn't exist.
//	@Tags			Workspace
//	@Accept			json
//	@Produce		json
//	@Param			request	body		WorkspaceContentUpdateRequest	true	"Request body"
//	@Success		200		{object}	WorkspaceItem
//	@Failure		400		{object}	ControlPlaneError
//	@Failure		401		{object}	ControlPlaneError
//	@Failure		403		{object}	ControlPlaneError
//	@Failure		404		{object}	ControlPlaneError
//	@Failure		429		{object}	ControlPlaneError
//	@Failure		500		{object}	ControlPlaneError
//	@Failure		503		{object}	ControlPlaneError
//	@Router			/v1/workspace/content [put]
func (h *WorkspaceHandler) UpdateContent(c *gin.Context) {
	var req WorkspaceContentUpdateRequest
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

	// Make sure req.Path is an absolute path
	if !strings.HasPrefix(req.Path, "/") {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "path must be an absolute path and start with /",
		})
		return
	}

	// Resolve the path
	absPath := filepath.Join(ws.Root, req.Path)

	// Check if the file exists
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if !req.Create {
				c.PureJSON(http.StatusNotFound, &ControlPlaneError{
					ErrorCode: ErrCodeGetWorkspaceContentFailed,
					Error:     "file not found",
				})
				return
			}
			// Create parent directories if they don't exist
			if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
				c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
					ErrorCode: ErrCodeGetWorkspaceContentFailed,
					Error:     fmt.Sprintf("failed to create parent directories: %v", err),
				})
				return
			}
			// Create empty file with default permissions
			fileInfo, err = os.Stat(absPath)
			if err == nil {
				// File was created by another process between our check and creation
				if fileInfo.IsDir() {
					c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
						ErrorCode: ErrCodeBadRequest,
						Error:     "path points to a directory, not a file",
					})
					return
				}
			} else {
				// Create the file
				f, err := os.Create(absPath)
				if err != nil {
					c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
						ErrorCode: ErrCodeGetWorkspaceContentFailed,
						Error:     fmt.Sprintf("failed to create file: %v", err),
					})
					return
				}
				f.Close()
				fileInfo, err = os.Stat(absPath)
				if err != nil {
					c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
						ErrorCode: ErrCodeGetWorkspaceContentFailed,
						Error:     fmt.Sprintf("failed to stat created file: %v", err),
					})
					return
				}
			}
		} else {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeGetWorkspaceContentFailed,
				Error:     err.Error(),
			})
			return
		}
	} else if fileInfo.IsDir() {
		c.PureJSON(http.StatusBadRequest, &ControlPlaneError{
			ErrorCode: ErrCodeBadRequest,
			Error:     "path points to a directory, not a file",
		})
		return
	}

	// Read existing content if needed
	var existingContent []byte
	if req.Mode != UpdateModeOverwrite {
		existingContent, err = os.ReadFile(absPath)
		if err != nil {
			c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
				ErrorCode: ErrCodeGetWorkspaceContentFailed,
				Error:     fmt.Sprintf("failed to read existing file: %v", err),
			})
			return
		}
	}

	// Prepare the new content based on the mode
	var newContent []byte
	switch req.Mode {
	case UpdateModeOverwrite:
		newContent = []byte(req.Content)
	case UpdateModeAppend:
		newContent = append(existingContent, []byte(req.Content)...)
	case UpdateModePrepend:
		newContent = append([]byte(req.Content), existingContent...)
	}

	// Write the content to the file
	if err := os.WriteFile(absPath, newContent, fileInfo.Mode()); err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeGetWorkspaceContentFailed,
			Error:     fmt.Sprintf("failed to write file: %v", err),
		})
		return
	}

	// Get updated file info
	updatedInfo, err := os.Stat(absPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeGetWorkspaceContentFailed,
			Error:     err.Error(),
		})
		return
	}

	// Get relative path for response
	relPath, err := filepath.Rel(ws.Root, absPath)
	if err != nil {
		c.PureJSON(http.StatusInternalServerError, &ControlPlaneError{
			ErrorCode: ErrCodeGetWorkspaceContentFailed,
			Error:     err.Error(),
		})
		return
	}
	relPath = filepath.Join("/", filepath.ToSlash(relPath))

	// Create response item
	item := WorkspaceItem{
		Id:           relPath,
		Name:         filepath.Base(absPath),
		Type:         WorkspaceItemTypeFile,
		Path:         relPath,
		AbsolutePath: absPath,
		CreatedAt:    updatedInfo.ModTime(),
		ModifiedAt:   updatedInfo.ModTime(),
		Size:         updatedInfo.Size(),
		SyncStatus:   SyncStatusHidden, // TODO: Replace with actual sync status
		Permissions:  []Permission{},   // TODO: Replace with actual permissions
		Children:     []WorkspaceItem{},
	}

	c.PureJSON(http.StatusOK, &item)
}
