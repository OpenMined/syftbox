package syftsdk

import (
	"context"
	"fmt"
	"net/http"

	"resty.dev/v3"
)

const (
	v1BlobUpload          = "/api/v1/blob/upload"
	v1BlobUploadPresigned = "/api/v1/blob/upload/presigned"
	v1BlobDownload        = "/api/v1/blob/download"
	v1BlobDelete          = "/api/v1/blob/delete"
)

type BlobAPI struct {
	client *resty.Client
}

func newBlobAPI(client *resty.Client) *BlobAPI {
	return &BlobAPI{
		client: client,
	}
}

// Upload uploads a file to the blob storage
func (b *BlobAPI) Upload(ctx context.Context, params *UploadParams) (*UploadResponse, error) {
	var resp UploadResponse
	var sdkError SyftSDKError

	res, err := b.client.R().
		SetContext(ctx).
		SetQueryParam("key", params.Key).
		// SetQueryParam("crc64nvme", params.ChecksumCRC64NVME).
		SetFile("file", params.FilePath).
		SetResult(&resp).
		SetError(&sdkError).
		Put(v1BlobUpload)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob upload: %q", err)
	}

	if res.IsError() {
		if res.StatusCode() == http.StatusForbidden {
			return nil, fmt.Errorf("%w: %s", ErrNoPermissions, sdkError.Error)
		}
		return nil, fmt.Errorf("sdk: blob upload: %q %q", res.Status(), sdkError.Error)
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
		SetResult(&resp).
		SetError(&sdkError).
		Post(v1BlobUploadPresigned)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob upload presigned: %q", err)
	}

	if res.IsError() {
		return nil, fmt.Errorf("sdk: blob upload presigned: %q %q", res.Status(), sdkError.Error)
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
		SetResult(&resp).
		SetError(&sdkError).
		Post(v1BlobDownload)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob download presigned: %q", err)
	}

	if res.IsError() {
		return nil, fmt.Errorf("sdk: blob download presigned: %q %q", res.Status(), sdkError.Error)
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
		SetResult(&resp).
		SetError(&sdkError).
		Post(v1BlobDelete)

	if err != nil {
		return nil, fmt.Errorf("sdk: blob delete: %q", err)
	}

	if res.IsError() {
		return nil, fmt.Errorf("sdk blob delete: %q %q", res.Status(), sdkError.Error)
	}

	return &resp, nil
}
