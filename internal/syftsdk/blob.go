package syftsdk

import (
	"context"
	"fmt"
	"log/slog"

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
func (b *BlobAPI) Upload(ctx context.Context, params *UploadParams) (*UploadResponse, error) {
	var resp UploadResponse
	var sdkError SyftSDKError

	if !utils.FileExists(params.FilePath) {
		return nil, ErrFileNotFound
	}

	res, err := b.client.R().
		SetContext(ctx).
		SetQueryParam("key", params.Key).
		// SetQueryParam("crc64nvme", params.ChecksumCRC64NVME).
		SetFile("file", params.FilePath).
		SetSuccessResult(&resp).
		SetErrorResult(&sdkError).
		SetUploadCallback(func(info req.UploadInfo) {
			slog.Debug("sdk: blob upload", "file", info.FileName, "progress", float64(info.UploadedSize)/float64(info.FileSize)*100.0)
		}).
		Put(v1BlobUpload)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob upload: %q", err)
	}

	if res.IsErrorState() {
		return nil, &sdkError
	}

	return &resp, nil
}

// UploadPresigned gets presigned URLs for uploading multiple blobs
func (b *BlobAPI) UploadPresigned(ctx context.Context, params *PresignedParams) (*PresignedResponse, error) {
	var resp PresignedResponse
	var sdkError SyftSDKError

	res, err := b.client.R().
		SetContext(ctx).
		SetBody(params).
		SetSuccessResult(&resp).
		SetErrorResult(&sdkError).
		Post(v1BlobUploadPresigned)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob upload presigned: %q", err)
	}

	if res.IsErrorState() {
		return nil, &sdkError
	}

	return &resp, nil
}

// DownloadPresigned gets presigned URLs for downloading multiple blobs
func (b *BlobAPI) DownloadPresigned(ctx context.Context, params *PresignedParams) (*PresignedResponse, error) {
	var resp PresignedResponse
	var sdkError SyftSDKError

	res, err := b.client.R().
		SetContext(ctx).
		SetBody(params).
		SetSuccessResult(&resp).
		SetErrorResult(&sdkError).
		Post(v1BlobDownload)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob download presigned: %q", err)
	}

	if res.IsErrorState() {
		return nil, &sdkError
	}

	return &resp, nil
}

// Delete deletes multiple blobs
func (b *BlobAPI) Delete(ctx context.Context, params *DeleteParams) (*DeleteResponse, error) {
	var resp DeleteResponse
	var sdkError SyftSDKError

	res, err := b.client.R().
		SetContext(ctx).
		SetBody(params).
		SetSuccessResult(&resp).
		SetErrorResult(&sdkError).
		Post(v1BlobDelete)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob delete: %q", err)
	}

	if res.IsErrorState() {
		return nil, &sdkError
	}

	return &resp, nil
}
