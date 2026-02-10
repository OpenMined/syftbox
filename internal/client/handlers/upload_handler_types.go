package handlers

import "time"

type UploadInfoResponse struct {
	ID             string    `json:"id"`
	Key            string    `json:"key"`
	LocalPath      string    `json:"localPath"`
	State          string    `json:"state"`
	Size           int64     `json:"size"`
	UploadedBytes  int64     `json:"uploadedBytes"`
	PartSize       int64     `json:"partSize,omitempty"`
	PartCount      int       `json:"partCount,omitempty"`
	CompletedParts []int     `json:"completedParts,omitempty"`
	Progress       float64   `json:"progress"`
	Error          string    `json:"error,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type UploadListResponse struct {
	Uploads []UploadInfoResponse `json:"uploads"`
}
