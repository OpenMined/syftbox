package blob

type UploadRequest struct {
	Name      string `json:"name" form:"name" binding:"required"`
	TotalSize uint64 `json:"size" form:"size" binding:"required"`
}

type UploadResponse struct {
	Name          string   `json:"name" binding:"required"`
	UploadID      string   `json:"uploadId"`
	PresignedURLs []string `json:"presignedUrls"`
}

type CompleteUploadRequest struct {
	Name     string           `json:"name"`
	UploadId string           `json:"uploadId"`
	Parts    []*CompletedPart `json:"parts"`
}

type CompletedPart struct {
	PartNumber uint16 `json:"partNumber"`
	ETag       string `json:"etag"`
}

type BlobInfo struct {
	Key          string `json:"key"`
	ETag         string `json:"etag"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
}
