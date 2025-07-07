package syftsdk

import (
	"context"

	"github.com/imroc/req/v3"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	v1BlobUpload          = "/api/v1/blob/upload"
	v1BlobUploadPresigned = "/api/v1/blob/upload/presigned"
	v1BlobDownload        = "/api/v1/blob/download"
	v1BlobDelete          = "/api/v1/blob/delete"
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

	resp, err := b.client.R().
		SetContext(ctx).
		SetQueryParam("key", params.Key).
		// SetQueryParam("crc64nvme", params.ChecksumCRC64NVME).
		SetRetryCount(0).
		SetFile("file", params.FilePath).
		SetSuccessResult(&apiResp).
		SetUploadCallback(func(info req.UploadInfo) {
			// if file size is less than 1MB, don't show progress
			if info.FileSize < 1024*1024*1 || params.Callback == nil {
				return
			}
			params.Callback(info.UploadedSize, info.FileSize)
		}).
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

// DownloadPresigned gets presigned URLs for downloading multiple blobs
func (b *BlobAPI) DownloadPresigned(ctx context.Context, params *PresignedParams) (apiResp *PresignedResponse, err error) {
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
