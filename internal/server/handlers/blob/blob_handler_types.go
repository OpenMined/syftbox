package blob

type BlobUrl struct {
	Key string `json:"key"`
	Url string `json:"url"`
}

type BlobError struct {
	Key   string `json:"key"`
	Error string `json:"error"`
}

type UploadRequest struct {
	Key string `form:"key" binding:"required"`
	// MD5       string `form:"md5"`
	// CRC64NVME string `form:"crc64nvme"`
	// CRC32C    string `form:"crc32c"`
	// SHA256    string `form:"sha256"`
}

type UploadResponse struct {
	Key          string `json:"key"`
	Version      string `json:"version"`
	ETag         string `json:"etag"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
}

type PresignUrlRequest struct {
	Keys []string `json:"keys" binding:"required,min=1"`
}

type PresignUrlResponse struct {
	URLs   []*BlobUrl   `json:"urls"`
	Errors []*BlobError `json:"errors"`
}

type DeleteRequest struct {
	Keys []string `json:"keys" binding:"required,min=1"`
}

type DeleteResponse struct {
	Deleted []string     `json:"deleted"`
	Errors  []*BlobError `json:"errors"`
}
