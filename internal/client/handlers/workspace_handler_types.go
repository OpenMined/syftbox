package handlers

import (
	"encoding/json"
	"strings"
	"time"
)

// SyncStatus represents the synchronization status of a workspace item
type SyncStatus string

const (
	SyncStatusSynced   SyncStatus = "synced"
	SyncStatusSyncing  SyncStatus = "syncing"
	SyncStatusPending  SyncStatus = "pending"
	SyncStatusRejected SyncStatus = "rejected"
	SyncStatusError    SyncStatus = "error"
	SyncStatusIgnored  SyncStatus = "ignored"
	SyncStatusHidden   SyncStatus = "hidden"
)

type WorkspaceItemType string

const (
	WorkspaceItemTypeFile   WorkspaceItemType = "file"
	WorkspaceItemTypeFolder WorkspaceItemType = "folder"
)

// WorkspaceItemsRequest represents the request parameters for listing workspace items
type WorkspaceItemsRequest struct {
	Path  string `form:"path"`
	Depth int    `form:"depth" binding:"min=0"`
}

// WorkspaceItem represents a file or folder in the workspace
type WorkspaceItem struct {
	Id          string            `json:"id"`
	Name        string            `json:"name"`
	Type        WorkspaceItemType `json:"type"`
	Path        string            `json:"path"`
	CreatedAt   time.Time         `json:"createdAt"`
	ModifiedAt  time.Time         `json:"modifiedAt"`
	Size        int64             `json:"size"`
	SyncStatus  SyncStatus        `json:"syncStatus"`
	Permissions []Permission      `json:"permissions"`
	Children    []WorkspaceItem   `json:"children"`
}

// NOTE:
// SyftBoxD APIs use Unix path notation (forward slashes) as the standard path format across
// all platforms for several important reasons:
//  1. Cross-platform compatibility: Since SyftBox is a networked application that runs across
//     different operating systems (Windows, macOS, Linux), using a consistent path format in
//     API responses ensures that paths work correctly regardless of the platform.
//  2. URL compatibility: Unix paths are compatible with URLs and web standards, making them
//     suitable for sharing links and bookmarks across the network.
//  3. Consistency: Using a single path format eliminates platform-specific path handling
//     issues and ensures that paths and shareable links remain valid across different systems.
//  4. Web standards: Forward slashes are the standard path separator in web technologies
//     and APIs, making Unix paths more natural for web-based applications.

// MarshalJSON implements json.Marshaler interface for WorkspaceItem
func (w WorkspaceItem) MarshalJSON() ([]byte, error) {
	type Alias WorkspaceItem
	return json.Marshal(&struct {
		Path string `json:"path"`
		*Alias
	}{
		Path:  strings.ReplaceAll(w.Path, "\\", "/"),
		Alias: (*Alias)(&w),
	})
}

// UnmarshalJSON implements json.Unmarshaler interface for WorkspaceItem
func (w *WorkspaceItem) UnmarshalJSON(data []byte) error {
	type Alias WorkspaceItem
	aux := &struct {
		Path string `json:"path"`
		*Alias
	}{
		Alias: (*Alias)(w),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	w.Path = strings.ReplaceAll(aux.Path, "\\", "/")
	return nil
}

// Permission represents a user's permission for a workspace item
type Permission struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Type   string `json:"type"` // "read", "write", or "admin"
	Avatar string `json:"avatar"`
}

// WorkspaceItemsResponse represents the response for listing workspace items
type WorkspaceItemsResponse struct {
	Items []WorkspaceItem `json:"items"`
}

// WorkspaceItemCreateRequest represents the request for creating a workspace item
type WorkspaceItemCreateRequest struct {
	Path      string            `json:"path" binding:"required"`
	Type      WorkspaceItemType `json:"type" binding:"required,oneof=file folder"`
	Overwrite bool              `json:"overwrite,omitempty" default:"false"`
}

// WorkspaceItemCreateResponse represents the response for creating a workspace item
type WorkspaceItemCreateResponse struct {
	Item WorkspaceItem `json:"item"`
}

// WorkspaceItemDeleteRequest represents the request for deleting workspace items
type WorkspaceItemDeleteRequest struct {
	Paths []string `json:"paths" binding:"required"`
}

// WorkspaceItemMoveRequest represents the request for moving a workspace item
type WorkspaceItemMoveRequest struct {
	// Full path to the source item
	SourcePath string `json:"sourcePath" binding:"required"`
	// Full path to the new item location, including the item name
	NewPath string `json:"newPath" binding:"required"`
	// Overwrite the destination item if it exists
	Overwrite bool `json:"overwrite,omitempty" default:"false"`
}

// WorkspaceItemMoveResponse represents the response for moving a workspace item
type WorkspaceItemMoveResponse struct {
	Item WorkspaceItem `json:"item"`
}

// WorkspaceItemCopyRequest represents the request for copying a workspace item
type WorkspaceItemCopyRequest struct {
	// Full path of the item to copy
	SourcePath string `json:"sourcePath" binding:"required"`
	// Full path of the new item location, including the item name
	NewPath string `json:"newPath" binding:"required"`
	// Overwrite the destination item if it exists
	Overwrite bool `json:"overwrite,omitempty" default:"false"`
}

// WorkspaceItemCopyResponse represents the response for copying a workspace item
type WorkspaceItemCopyResponse struct {
	Item WorkspaceItem `json:"item"`
}

// UpdateMode represents how the content should be updated
type UpdateMode string

const (
	UpdateModeOverwrite UpdateMode = "overwrite" // Replace entire file content
	UpdateModeAppend    UpdateMode = "append"    // Add content to end of file
	UpdateModePrepend   UpdateMode = "prepend"   // Add content to start of file
)

// WorkspaceContentRequest represents the request parameters for getting file content
type WorkspaceContentRequest struct {
	Path string `form:"path" binding:"required"`
}

// WorkspaceContentUpdateRequest represents the request for updating file content
type WorkspaceContentUpdateRequest struct {
	Path    string     `json:"path" binding:"required"`
	Content string     `json:"content" binding:"required"`
	Mode    UpdateMode `json:"mode" binding:"required,oneof=overwrite append prepend" default:"overwrite"`
	Create  bool       `json:"create" default:"false"` // Create file if it doesn't exist
}

// WorkspaceConflictError represents an error response when there is a conflict with an existing item
type WorkspaceConflictError struct {
	ErrorCode    string        `json:"errorCode"`
	Error        string        `json:"error"`
	ExistingItem WorkspaceItem `json:"existingItem"`
}
