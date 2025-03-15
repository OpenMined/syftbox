package syftapi

type SyftAPIError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// ========================================================================

// DataSiteViewInput represents the input parameters for the datasite view API
type GetDatasiteViewInput struct {
	// User string
}

// DataSiteViewOutput represents the output from the datasite view API
type GetDatasiteViewOutput struct {
	Files []BlobInfo `json:"files"`
}

// ========================================================================

type GetFileURLInput struct {
	User string   `json:"user"`
	Keys []string `json:"keys"`
}

type GetFileURLOutput struct {
	URLs   []BlobUrl   `json:"urls"`
	Errors []BlobError `json:"errors"`
}

// ========================================================================

type BlobInfo struct {
	Key          string `json:"key"`
	ETag         string `json:"etag"`
	Size         int    `json:"size"`
	LastModified string `json:"lastModified"`
}

type BlobUrl struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

type BlobError struct {
	Key   string `json:"key"`
	Error string `json:"error"`
}
