package blob

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	chunkSize          = 8 * 1024 * 1024 // 8MB
	multipartThreshold = 8 * 1024 * 1024
	uploadExpiry       = 15 * time.Minute
)

type BlobClient struct {
	s3Client    *s3.Client
	s3Presigner *s3.PresignClient
	bucketName  string
}

func NewBlobClient(s3Client *s3.Client, bucketName string) *BlobClient {
	s3Presigner := s3.NewPresignClient(s3Client)
	return &BlobClient{
		s3Client:    s3Client,
		s3Presigner: s3Presigner,
		bucketName:  bucketName,
	}
}

func NewBlobClientWithConfig(cfg *BlobConfig) *BlobClient {
	awsCfg := aws.Config{
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		Region:      cfg.Region,
	}

	awsClient := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
	})

	return NewBlobClient(awsClient, cfg.BucketName)
}

func (s *BlobClient) Download(ctx context.Context, key string) (*s3.GetObjectOutput, error) {
	return s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucketName,
		Key:    &key,
	})
}

func (s *BlobClient) ListObjects(ctx context.Context) ([]*BlobInfo, error) {
	var objects []*BlobInfo

	input := &s3.ListObjectsV2Input{
		Bucket: &s.bucketName,
	}

	// Create a paginator from the ListObjectsV2 API
	paginator := s3.NewListObjectsV2Paginator(s.s3Client, input)

	// Iterate through all pages
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, obj := range page.Contents {
			objects = append(objects, &BlobInfo{
				Key:          aws.ToString(obj.Key),
				ETag:         strings.ReplaceAll(aws.ToString(obj.ETag), "\"", ""),
				Size:         aws.ToInt64(obj.Size),
				LastModified: obj.LastModified.Format(time.RFC3339),
			})
		}
	}

	return objects, nil
}

func (s *BlobClient) PresignedUpload(ctx context.Context, req *UploadRequest) (*UploadResponse, error) {
	if req.TotalSize <= multipartThreshold {
		url, err := s.generateSingleUploadURL(ctx, req.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to generate upload URL: %w", err)
		}
		return &UploadResponse{
			Name:          req.Name,
			UploadID:      "",
			PresignedURLs: []string{url},
		}, nil
	}

	uploadID, err := s.createMultipartUpload(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate upload: %w", err)
	}

	urls, err := s.generatePartUploadURLs(ctx, req.Name, uploadID, req.TotalSize)
	if err != nil {
		return nil, fmt.Errorf("failed to generate upload URLs: %w", err)
	}

	return &UploadResponse{
		Name:          req.Name,
		UploadID:      uploadID,
		PresignedURLs: urls,
	}, nil
}

func (s *BlobClient) CompleteUpload(ctx context.Context, req *CompleteUploadRequest) error {
	return s.completeMultipartUpload(ctx, req.Name, req.UploadId, req.Parts)
}

func (s *BlobClient) PresignedDownload(ctx context.Context, key string) (string, error) {
	return s.generatePresignedDownloadURL(ctx, key)
}

func (s *BlobClient) PresignedBatchUpload(ctx context.Context, requests []*UploadRequest) ([]*UploadResponse, error) {
	uploads := make([]*UploadResponse, 0, len(requests))
	for _, req := range requests {
		upload, err := s.PresignedUpload(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to process %s: %w", req.Name, err)
		}
		uploads = append(uploads, upload)
	}
	return uploads, nil
}

func (s *BlobClient) PresignedBatchDownload(ctx context.Context, keys []string) ([]string, error) {
	downloads := make([]string, 0, len(keys))
	for _, filename := range keys {
		download, err := s.PresignedDownload(ctx, filename)
		if err != nil {
			return nil, fmt.Errorf("failed to generate URL for %s: %w", filename, err)
		}
		downloads = append(downloads, download)
	}
	return downloads, nil
}

func (s *BlobClient) createMultipartUpload(ctx context.Context, key string) (string, error) {
	result, err := s.s3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: &s.bucketName,
		Key:    &key,
	})
	if err != nil {
		return "", err
	}
	return *result.UploadId, nil
}

func (s *BlobClient) generateSingleUploadURL(ctx context.Context, key string) (string, error) {
	url, err := s.s3Presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucketName,
		Key:    &key,
	}, func(opts *s3.PresignOptions) {
		opts.Expires = uploadExpiry
	})
	if err != nil {
		return "", err
	}
	return url.URL, nil
}

func (s *BlobClient) generatePartUploadURLs(ctx context.Context, key string, uploadID string, totalSize uint64) ([]string, error) {
	numParts := uint16((totalSize + chunkSize - 1) / chunkSize)
	if numParts > 10000 {
		return nil, fmt.Errorf("total parts %d exceeds maximum allowed (10000)", numParts)
	}

	urls := make([]string, 0, numParts)
	for partNumber := uint16(1); partNumber <= numParts; partNumber++ {
		url, err := s.s3Presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
			Bucket:     &s.bucketName,
			Key:        &key,
			UploadId:   &uploadID,
			PartNumber: aws.Int32(int32(partNumber)),
		}, func(opts *s3.PresignOptions) {
			opts.Expires = 2 * uploadExpiry
		})
		if err != nil {
			return nil, err
		}

		urls = append(urls, url.URL)
	}
	return urls, nil
}

func (s *BlobClient) generatePresignedDownloadURL(ctx context.Context, key string) (string, error) {
	url, err := s.s3Presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucketName,
		Key:    &key,
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Hour
	})
	if err != nil {
		return "", err
	}
	return url.URL, nil
}

func (s *BlobClient) completeMultipartUpload(ctx context.Context, key string, uploadID string, parts []*CompletedPart) error {
	completedParts := make([]types.CompletedPart, len(parts))
	for i, part := range parts {
		completedParts[i] = types.CompletedPart{
			ETag:       &part.ETag,
			PartNumber: aws.Int32(int32(part.PartNumber)),
		}
	}

	_, err := s.s3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   &s.bucketName,
		Key:      &key,
		UploadId: &uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	return err
}
