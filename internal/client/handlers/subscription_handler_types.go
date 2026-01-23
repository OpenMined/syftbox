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
