package handlers

import "time"

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
	Path string            `json:"path" binding:"required"`
	Type WorkspaceItemType `json:"type" binding:"required,oneof=file folder"`
}

// WorkspaceItemCreateResponse represents the response for creating a workspace item
type WorkspaceItemCreateResponse struct {
	Item WorkspaceItem `json:"item"`
}

// WorkspaceItemDeleteRequest represents the request for deleting workspace items
type WorkspaceItemDeleteRequest struct {
	Paths []string `json:"paths" binding:"required"`
}
