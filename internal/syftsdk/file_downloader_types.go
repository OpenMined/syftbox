package syftsdk

const (
	AutoDetectWorkers = 0
	DefaultWorkers    = 8
)

const (
	CodePresignedURLErrors    = "E_PRESIGNED_URL"            // prefix for all presigned url errors
	CodePresignedURLExpired   = "E_PRESIGNED_URL_EXPIRED"    // presigned URL has expired
	CodePresignedURLInvalid   = "E_PRESIGNED_URL_INVALID"    // presigned URL is malformed or invalid
	CodePresignedURLForbidden = "E_PRESIGNED_URL_FORBIDDEN"  // access denied to presigned URL
	CodePresignedURLNotFound  = "E_PRESIGNED_URL_NOT_FOUND"  // object not found via presigned URL
	CodePresignedURLRateLimit = "E_PRESIGNED_URL_RATE_LIMIT" // rate limited by S3
)

type DownloadJob struct {
	URL       string // url to download from
	TargetDir string // directory to save the file to
	Name      string // name to save the file as
	Callback  func(job *DownloadJob, downloadedBytes int64, totalBytes int64)
}

type DownloadResult struct {
	DownloadJob
	DownloadPath string
	Error        error
}

type DownloadOpts struct {
	Workers int
	Jobs    []*DownloadJob
}
