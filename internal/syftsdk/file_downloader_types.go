package syftsdk

const (
	AutoDetectWorkers = 0
	DefaultWorkers    = 8
)

type DownloadJob struct {
	URL      string
	FileName string
}

type DownloadResult struct {
	DownloadJob
	DownloadPath string
	Error        error
}
