package syftsdk

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imroc/req/v3"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	v1BlobUpload             = "/api/v1/blob/upload"
	v1BlobUploadPresigned    = "/api/v1/blob/upload/presigned"
	v1BlobDownload           = "/api/v1/blob/download"
	v1BlobDelete             = "/api/v1/blob/delete"
	v1BlobUploadMultipart    = "/api/v1/blob/upload/multipart"
	v1BlobUploadComplete     = "/api/v1/blob/upload/complete"
	multipartUploadThreshold = int64(32 * 1024 * 1024) // switch to resumable uploads for larger files
)

type BlobAPI struct {
	client *req.Client
}

func newBlobAPI(client *req.Client) *BlobAPI {
	return &BlobAPI{
		client: client,
	}
}

// Upload uploads a file to the blob storage
func (b *BlobAPI) Upload(ctx context.Context, params *UploadParams) (apiResp *UploadResponse, err error) {
	if !utils.FileExists(params.FilePath) {
		return nil, ErrFileNotFound
	}

	info, err := os.Stat(params.FilePath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	if params.ResumeDir == "" && info.Size() <= multipartUploadThreshold {
		return b.uploadSingle(ctx, params)
	}

	uploader := newResumableUploader(b.client, params, info)
	return uploader.Upload(ctx)
}

func (b *BlobAPI) uploadSingle(ctx context.Context, params *UploadParams) (apiResp *UploadResponse, err error) {
	resp, err := b.client.R().
		SetContext(ctx).
		SetQueryParam("key", params.Key).
		// SetQueryParam("crc64nvme", params.ChecksumCRC64NVME).
		SetRetryCount(0).
		SetFile("file", params.FilePath).
		SetSuccessResult(&apiResp).
		SetUploadCallbackWithInterval(func(info req.UploadInfo) {
			// if file size is less than 1MB, don't show progress
			if info.FileSize < 1024*1024*1 || params.Callback == nil {
				return
			}
			params.Callback(info.UploadedSize, info.FileSize)
		}, time.Second).
		Put(v1BlobUpload)

	if err := handleAPIError(resp, err, "blob upload"); err != nil {
		return nil, err
	}

	return apiResp, nil
}

// UploadPresigned gets presigned URLs for uploading multiple blobs
func (b *BlobAPI) UploadPresigned(ctx context.Context, params *PresignedParams) (apiResp *PresignedResponse, err error) {
	resp, err := b.client.R().
		SetContext(ctx).
		SetBody(params).
		SetSuccessResult(&apiResp).
		Post(v1BlobUploadPresigned)

	if err := handleAPIError(resp, err, "blob upload presigned"); err != nil {
		return nil, err
	}

	return apiResp, nil
}

// Download gets presigned URLs for downloading multiple blobs
func (b *BlobAPI) Download(ctx context.Context, params *PresignedParams) (apiResp *PresignedResponse, err error) {
	// if no keys are provided, return an error
	if len(params.Keys) == 0 {
		return nil, fmt.Errorf("no keys provided")
	}

	resp, err := b.client.R().
		SetContext(ctx).
		SetBody(params).
		SetSuccessResult(&apiResp).
		Post(v1BlobDownload)

	if err := handleAPIError(resp, err, "blob download presigned"); err != nil {
		return nil, err
	}

	return apiResp, nil
}

// Delete deletes multiple blobs
func (b *BlobAPI) Delete(ctx context.Context, params *DeleteParams) (apiResp *DeleteResponse, err error) {
	resp, err := b.client.R().
		SetContext(ctx).
		SetBody(params).
		SetSuccessResult(&apiResp).
		Post(v1BlobDelete)

	if err := handleAPIError(resp, err, "blob delete"); err != nil {
		return nil, err
	}

	return apiResp, nil
}
