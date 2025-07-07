package syftsdk

// ===================================================================================================

// DataSiteViewInput represents the input parameters for the datasite view API
// for now, no params are needed
type DatasiteViewParams struct{}

// DataSiteViewOutput represents the output from the datasite view API
type DatasiteViewResponse struct {
	Files []BlobInfo `json:"files"`
}

// ===================================================================================================

// DownloadFileParams represents the parameters for downloading files
type DownloadFileParams struct {
	User string   `json:"user"`
	Keys []string `json:"keys"`
}

// DownloadFileResponse represents the response from a file download request
type DownloadFileResponse struct {
	URLs   []*BlobURL   `json:"urls"`
	Errors []*BlobError `json:"errors"`
}
