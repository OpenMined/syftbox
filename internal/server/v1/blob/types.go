package blob

import "github.com/yashgorana/syftbox-go/internal/blob"

type CompleteRequest struct {
	blob.CompleteUploadRequest
}

type UploadRequest struct {
	blob.UploadRequest
}

type UploadAccept struct {
	blob.UploadResponse
}
