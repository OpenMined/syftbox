package blob

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	chunkSize          = 8 * 1024 * 1024   // 8MB
	multipartThreshold = 128 * 1024 * 1024 // 64MB
	uploadExpiry       = 15 * time.Minute
)

type BlobService struct {
	s3Client    *s3.Client
	s3Presigner *s3.PresignClient
	bucketName  string
}

func NewBlobService(s3Client *s3.Client, bucketName string) *BlobService {
	s3Presigner := s3.NewPresignClient(s3Client)
	return &BlobService{
		s3Client:    s3Client,
		s3Presigner: s3Presigner,
		bucketName:  bucketName,
	}
}

func (s *BlobService) ListObjects(ctx context.Context) ([]*BlobInfo, error) {
	var objects []*BlobInfo
	var continuationToken *string

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            &s.bucketName,
			ContinuationToken: continuationToken,
		}

		result, err := s.s3Client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, err
		}

		for _, obj := range result.Contents {
			objects = append(objects, &BlobInfo{
				Key:          *obj.Key,
				ETag:         strings.ReplaceAll(*obj.ETag, "\"", ""),
				Size:         uint64(*obj.Size),
				LastModified: obj.LastModified.Format(time.RFC3339),
			})
		}

		if !(*result.IsTruncated) {
			break
		}
		continuationToken = result.NextContinuationToken
	}

	return objects, nil
}

func (s *BlobService) PresignedUpload(ctx context.Context, req *FileUploadInput) (*FileUploadOutput, error) {
	if req.TotalSize < multipartThreshold {
		url, err := s.generateSingleUploadURL(ctx, req.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to generate upload URL: %w", err)
		}
		return &FileUploadOutput{
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

	return &FileUploadOutput{
		Name:          req.Name,
		UploadID:      uploadID,
		PresignedURLs: urls,
	}, nil
}

func (s *BlobService) CompleteUpload(ctx context.Context, req *CompleteUploadInput) error {
	return s.completeMultipartUpload(ctx, req.Name, req.UploadId, req.Parts)
}

func (s *BlobService) PresignedBatchUpload(ctx context.Context, requests []*FileUploadInput) ([]*FileUploadOutput, error) {
	uploads := make([]*FileUploadOutput, 0, len(requests))
	for _, req := range requests {
		upload, err := s.PresignedUpload(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to process %s: %w", req.Name, err)
		}
		uploads = append(uploads, upload)
	}
	return uploads, nil
}

func (s *BlobService) PresignedDownload(ctx context.Context, key string) (string, error) {
	return s.generatePresignedDownloadURL(ctx, key)
}

func (s *BlobService) PresignedBatchDownload(ctx context.Context, keys []string) ([]string, error) {
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

func (s *BlobService) createMultipartUpload(ctx context.Context, key string) (string, error) {
	result, err := s.s3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: &s.bucketName,
		Key:    &key,
	})
	if err != nil {
		return "", err
	}
	return *result.UploadId, nil
}

func (s *BlobService) generateSingleUploadURL(ctx context.Context, key string) (string, error) {
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

func (s *BlobService) generatePartUploadURLs(ctx context.Context, key string, uploadID string, totalSize uint64) ([]string, error) {
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

func (s *BlobService) generatePresignedDownloadURL(ctx context.Context, key string) (string, error) {
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

func (s *BlobService) completeMultipartUpload(ctx context.Context, key string, uploadID string, parts []*CompletedPart) error {
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
