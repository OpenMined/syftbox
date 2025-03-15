package datasite

import "github.com/yashgorana/syftbox-go/internal/datasite"

type DownloadRequest struct {
	Keys []string `json:"keys"`
}

type DownloadResponse struct {
	URLs   []datasite.BlobUrl   `json:"urls"`
	Errors []datasite.BlobError `json:"errors"`
}
