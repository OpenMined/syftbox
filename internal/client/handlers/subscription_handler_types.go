package handlers

import "time"

type SubscriptionRule struct {
	Action   string `json:"action"`
	Datasite string `json:"datasite,omitempty"`
	Path     string `json:"path"`
}

type SubscriptionConfig struct {
	Version  int                `json:"version"`
	Defaults map[string]string  `json:"defaults"`
	Rules    []SubscriptionRule `json:"rules"`
}

type SubscriptionsResponse struct {
	Path   string             `json:"path"`
	Config SubscriptionConfig `json:"config"`
}

type SubscriptionsUpdateRequest struct {
	Config SubscriptionConfig `json:"config"`
}

type SubscriptionsRuleRequest struct {
	Rule SubscriptionRule `json:"rule"`
}

type DiscoveryFile struct {
	Path         string    `json:"path"`
	ETag         string    `json:"etag"`
	Size         int       `json:"size"`
	LastModified time.Time `json:"lastModified"`
	Action       string    `json:"action"`
}

type DiscoveryResponse struct {
	Files []DiscoveryFile `json:"files"`
}

type EffectiveFile struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Allowed bool   `json:"allowed"`
}

type EffectiveResponse struct {
	Files []EffectiveFile `json:"files"`
}

type SyncQueueResponse struct {
	Files []SyncFileStatus `json:"files"`
}

type PublicationEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type PublicationsResponse struct {
	Files []PublicationEntry `json:"files"`
}

// MarkedFileInfo contains information about a conflict or rejected file
type MarkedFileInfo struct {
	Path         string    `json:"path"`
	MarkerType   string    `json:"markerType"`
	OriginalPath string    `json:"originalPath"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"modTime"`
}

// ConflictsResponse contains lists of conflict and rejected files
type ConflictsResponse struct {
	Conflicts []MarkedFileInfo `json:"conflicts"`
	Rejected  []MarkedFileInfo `json:"rejected"`
	Summary   ConflictsSummary `json:"summary"`
}

type ConflictsSummary struct {
	ConflictCount int `json:"conflictCount"`
	RejectedCount int `json:"rejectedCount"`
}

// CleanupResponse contains results of a cleanup operation
type CleanupResponse struct {
	CleanedCount int      `json:"cleanedCount"`
	Errors       []string `json:"errors,omitempty"`
}
