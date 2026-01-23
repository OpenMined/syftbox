package blob

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	uploadExpiry   = 5 * time.Minute
	downloadExpiry = 5 * time.Minute
)

var (
	ErrInvalidKey = errors.New("invalid key")
)

type S3Backend struct {
	s3Client    *s3.Client
	s3Presigner *s3.PresignClient
	config      *S3Config
	hooks       *blobBackendHooks
}

func NewS3Backend(s3Client *s3.Client, config *S3Config) *S3Backend {
	s3Presigner := s3.NewPresignClient(s3Client)
	return &S3Backend{
		s3Client:    s3Client,
		s3Presigner: s3Presigner,
		config:      config,
		hooks: &blobBackendHooks{
			AfterPutObject:    nil,
			AfterDeleteObject: nil,
			AfterCopyObject:   nil,
		},
	}
}

func NewS3BackendWithConfig(cfg *S3Config) *S3Backend {
	// Create optimized HTTP client with HTTP/2 support
	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		Timeout: 30 * time.Second,
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
		config.WithRegion(cfg.Region),
		config.WithHTTPClient(httpClient),
		config.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(o *retry.StandardOptions) {
				o.MaxAttempts = 10
			})
		}),
	)
	if err != nil {
		panic("failed to load AWS config: " + err.Error())
	}

	// Configure S3 client with additional options
	awsClient := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
		if cfg.UseAccelerate {
			o.UseAccelerate = true
		}
	})

	return NewS3Backend(awsClient, cfg)
}

func (s *S3Backend) setHooks(hooks *blobBackendHooks) {
	if hooks != nil {
		s.hooks.AfterPutObject = hooks.AfterPutObject
		s.hooks.AfterDeleteObject = hooks.AfterDeleteObject
		s.hooks.AfterCopyObject = hooks.AfterCopyObject
	}
}

// ===================================================================================================

func (s *S3Backend) GetObject(ctx context.Context, key string) (*GetObjectResponse, error) {
	resp, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:       &s.config.BucketName,
		Key:          &key,
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return nil, err
	}

	return &GetObjectResponse{
		Body:         resp.Body,
		Size:         aws.ToInt64(resp.ContentLength),
		ETag:         strings.ReplaceAll(aws.ToString(resp.ETag), "\"", ""),
		LastModified: aws.ToTime(resp.LastModified),
	}, nil
}

func (s *S3Backend) GetObjectPresigned(ctx context.Context, key string) (string, error) {
	return s.generateGetObjectURL(ctx, key)
}

// ===================================================================================================

// Add an object to a bucket
func (s *S3Backend) PutObject(ctx context.Context, params *PutObjectParams) (*PutObjectResponse, error) {
	if !ValidateKey(params.Key) {
		return nil, ErrInvalidKey
	}

	s3Params := &s3.PutObjectInput{
		Bucket:        &s.config.BucketName,
		Key:           &params.Key,
		Body:          params.Body,
		ContentLength: aws.Int64(params.Size),
	}

	resp, err := s.s3Client.PutObject(ctx, s3Params)
	if err != nil {
		return nil, err
	}

	// s3.PutObjectOutput does not have LastModified
	result := &PutObjectResponse{
		Key:          params.Key,
		Size:         params.Size,
		Version:      aws.ToString(resp.VersionId),
		ETag:         strings.ReplaceAll(aws.ToString(resp.ETag), "\"", ""),
		LastModified: time.Now().UTC(),
	}

	if s.hooks.AfterPutObject != nil {
		s.hooks.AfterPutObject(params, result)
	}

	return result, nil
}

func (s *S3Backend) PutObjectPresigned(ctx context.Context, key string) (string, error) {
	return s.generatePutObjectURL(ctx, key)
}

func (s *S3Backend) PutObjectMultipart(ctx context.Context, params *PutObjectMultipartParams) (*PutObjectMultipartResponse, error) {
	if !ValidateKey(params.Key) {
		return nil, ErrInvalidKey
	}

	uploadID := params.UploadID
	if uploadID == "" {
		// Create a new multipart upload
		result, err := s.s3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: &s.config.BucketName,
			Key:    &params.Key,
		})
		if err != nil {
			return nil, err
		}
		uploadID = aws.ToString(result.UploadId)
	}

	parts := params.PartNumbers
	if len(parts) == 0 {
		if params.Parts == 0 {
			return nil, fmt.Errorf("parts not specified for multipart upload")
		}
		for i := 0; i < int(params.Parts); i++ {
			parts = append(parts, i+1)
		}
	}

	urls := make(map[int]string, len(parts))
	for _, partNum := range parts {
		pn := int32(partNum)
		url, err := s.s3Presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
			Bucket:     &s.config.BucketName,
			Key:        &params.Key,
			UploadId:   aws.String(uploadID),
			PartNumber: aws.Int32(pn),
		}, func(opts *s3.PresignOptions) {
			opts.Expires = 2 * uploadExpiry
		})
		if err != nil {
			return nil, err
		}
		urls[partNum] = url.URL
	}

	return &PutObjectMultipartResponse{
		Key:      params.Key,
		UploadID: uploadID,
		URLs:     urls,
	}, nil
}

func (s *S3Backend) CompleteMultipartUpload(ctx context.Context, params *CompleteMultipartUploadParams) (*PutObjectResponse, error) {
	if !ValidateKey(params.Key) {
		return nil, ErrInvalidKey
	}

	completedParts := make([]types.CompletedPart, len(params.Parts))
	for i, part := range params.Parts {
		completedParts[i] = types.CompletedPart{
			ETag:       &part.ETag,
			PartNumber: aws.Int32(int32(part.PartNumber)),
		}
	}

	res, err := s.s3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   &s.config.BucketName,
		Key:      &params.Key,
		UploadId: &params.UploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return nil, err
	}

	result := &PutObjectResponse{
		Key:          params.Key,
		Version:      aws.ToString(res.VersionId),
		ETag:         strings.ReplaceAll(aws.ToString(res.ETag), "\"", ""),
		LastModified: time.Now().UTC(),
	}

	if head, headErr := s.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.config.BucketName,
		Key:    &params.Key,
	}); headErr == nil {
		result.Size = aws.ToInt64(head.ContentLength)
		result.LastModified = aws.ToTime(head.LastModified)
	}

	if s.hooks.AfterPutObject != nil {
		s.hooks.AfterPutObject(&PutObjectParams{
			Key:  params.Key,
			Size: result.Size,
		}, result)
	}

	return result, nil
}

func (s *S3Backend) AbortMultipartUpload(ctx context.Context, params *AbortMultipartUploadParams) error {
	if !ValidateKey(params.Key) {
		return ErrInvalidKey
	}

	_, err := s.s3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   &s.config.BucketName,
		Key:      &params.Key,
		UploadId: &params.UploadID,
	})
	return err
}

// ===================================================================================================

func (s *S3Backend) CopyObject(ctx context.Context, params *CopyObjectParams) (*CopyObjectResponse, error) {
	if !ValidateKey(params.SourceKey) {
		return nil, fmt.Errorf("invalid source key: %s", params.SourceKey)
	}
	if !ValidateKey(params.DestinationKey) {
		return nil, fmt.Errorf("invalid destination key: %s", params.DestinationKey)
	}
	resp, err := s.s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     &s.config.BucketName,
		CopySource: aws.String(fmt.Sprintf("%s/%s", s.config.BucketName, params.SourceKey)),
		Key:        &params.DestinationKey,
		// we can use these later!
		// CopySourceIfMatch: ,
		// CopySourceIfModifiedSince: ,
	})
	if err != nil {
		return nil, err
	}

	result := &CopyObjectResponse{
		ETag:         strings.ReplaceAll(aws.ToString(resp.CopyObjectResult.ETag), "\"", ""),
		LastModified: aws.ToTime(resp.CopyObjectResult.LastModified),
	}

	if s.hooks.AfterCopyObject != nil {
		s.hooks.AfterCopyObject(params, result)
	}

	return result, nil
}

// ===================================================================================================

func (s *S3Backend) DeleteObject(ctx context.Context, key string) (bool, error) {
	_, err := s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.config.BucketName,
		Key:    &key,
	})
	if err != nil {
		return false, err
	}
	if s.hooks.AfterDeleteObject != nil {
		s.hooks.AfterDeleteObject(key, true)
	}
	return true, nil
}

// ===================================================================================================

func (s *S3Backend) ListObjects(ctx context.Context) ([]*BlobInfo, error) {
	var objects []*BlobInfo

	// Create a paginator from the ListObjectsV2 API
	paginator := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: &s.config.BucketName,
	})

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

// ===================================================================================================

func (s *S3Backend) generatePutObjectURL(ctx context.Context, key string) (string, error) {
	if !ValidateKey(key) {
		return "", ErrInvalidKey
	}

	url, err := s.s3Presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.config.BucketName,
		Key:    &key,
	}, func(opts *s3.PresignOptions) {
		opts.Expires = uploadExpiry
	})
	if err != nil {
		return "", err
	}
	return url.URL, nil
}

func (s *S3Backend) generateGetObjectURL(ctx context.Context, key string) (string, error) {
	if !ValidateKey(key) {
		return "", ErrInvalidKey
	}

	url, err := s.s3Presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.config.BucketName,
		Key:    &key,
	}, func(opts *s3.PresignOptions) {
		opts.Expires = downloadExpiry
	})
	if err != nil {
		return "", err
	}
	return url.URL, nil
}

func (s *S3Backend) Delegate() any {
	return s.s3Client
}

// check if BlobClient implements IBlobClient interface
var _ IBlobBackend = (*S3Backend)(nil)
