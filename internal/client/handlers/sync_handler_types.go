package handlers

import "time"

type SyncFileStatus struct {
	Path          string    `json:"path"`
	State         string    `json:"state"`
	ConflictState string    `json:"conflictState,omitempty"`
	Progress      float64   `json:"progress"`
	Error         string    `json:"error,omitempty"`
	ErrorCount    int       `json:"errorCount,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type SyncSummary struct {
	Pending   int `json:"pending"`
	Syncing   int `json:"syncing"`
	Completed int `json:"completed"`
	Error     int `json:"error"`
}

type SyncStatusResponse struct {
	Files   []SyncFileStatus `json:"files"`
	Summary SyncSummary      `json:"summary"`
}
