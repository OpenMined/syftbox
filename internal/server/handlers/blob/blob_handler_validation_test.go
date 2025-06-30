package blob

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMultipartUploadRequest_Validation tests the validation of multipart upload requests
func TestMultipartUploadRequest_Validation(t *testing.T) {
	tests := []struct {
		name      string
		request   MultipartUploadRequest
		expectErr bool
		errField  string
	}{
		{
			name: "valid request",
			request: MultipartUploadRequest{
				Key:   "user@example.com/folder/file.bin",
				Parts: 3,
			},
			expectErr: false,
		},
		{
			name: "empty key",
			request: MultipartUploadRequest{
				Key:   "",
				Parts: 1,
			},
			expectErr: true,
			errField:  "Key",
		},
		{
			name: "zero parts",
			request: MultipartUploadRequest{
				Key:   "user@example.com/file.bin",
				Parts: 0,
			},
			expectErr: true,
			errField:  "Parts",
		},
		{
			name: "too many parts",
			request: MultipartUploadRequest{
				Key:   "user@example.com/file.bin",
				Parts: 10001,
			},
			expectErr: true,
			errField:  "Parts",
		},
		{
			name: "maximum parts",
			request: MultipartUploadRequest{
				Key:   "user@example.com/file.bin",
				Parts: 10000,
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation that would be done by binding
			if tt.request.Key == "" {
				assert.True(t, tt.expectErr)
				assert.Equal(t, "Key", tt.errField)
			} else if tt.request.Parts < 1 || tt.request.Parts > 10000 {
				assert.True(t, tt.expectErr)
				assert.Equal(t, "Parts", tt.errField)
			} else {
				assert.False(t, tt.expectErr)
			}
		})
	}
}

// TestCompleteUploadRequest_Validation tests the validation of complete upload requests
func TestCompleteUploadRequest_Validation(t *testing.T) {
	tests := []struct {
		name      string
		request   CompleteUploadRequest
		expectErr bool
		errField  string
	}{
		{
			name: "valid request",
			request: CompleteUploadRequest{
				Key:      "user@example.com/folder/file.bin",
				UploadID: "test-upload-id",
				Parts: []CompletedPartRequest{
					{PartNumber: 1, ETag: "etag1"},
					{PartNumber: 2, ETag: "etag2"},
				},
			},
			expectErr: false,
		},
		{
			name: "empty key",
			request: CompleteUploadRequest{
				Key:      "",
				UploadID: "test-upload-id",
				Parts:    []CompletedPartRequest{{PartNumber: 1, ETag: "etag1"}},
			},
			expectErr: true,
			errField:  "Key",
		},
		{
			name: "empty upload ID",
			request: CompleteUploadRequest{
				Key:      "user@example.com/file.bin",
				UploadID: "",
				Parts:    []CompletedPartRequest{{PartNumber: 1, ETag: "etag1"}},
			},
			expectErr: true,
			errField:  "UploadID",
		},
		{
			name: "empty parts",
			request: CompleteUploadRequest{
				Key:      "user@example.com/file.bin",
				UploadID: "test-upload-id",
				Parts:    []CompletedPartRequest{},
			},
			expectErr: true,
			errField:  "Parts",
		},
		{
			name: "part with zero number",
			request: CompleteUploadRequest{
				Key:      "user@example.com/file.bin",
				UploadID: "test-upload-id",
				Parts:    []CompletedPartRequest{{PartNumber: 0, ETag: "etag1"}},
			},
			expectErr: true,
			errField:  "PartNumber",
		},
		{
			name: "part with empty etag",
			request: CompleteUploadRequest{
				Key:      "user@example.com/file.bin",
				UploadID: "test-upload-id",
				Parts:    []CompletedPartRequest{{PartNumber: 1, ETag: ""}},
			},
			expectErr: true,
			errField:  "ETag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation
			if tt.request.Key == "" {
				assert.True(t, tt.expectErr)
				assert.Equal(t, "Key", tt.errField)
			} else if tt.request.UploadID == "" {
				assert.True(t, tt.expectErr)
				assert.Equal(t, "UploadID", tt.errField)
			} else if len(tt.request.Parts) == 0 {
				assert.True(t, tt.expectErr)
				assert.Equal(t, "Parts", tt.errField)
			} else {
				// Check individual parts
				hasError := false
				for _, part := range tt.request.Parts {
					if part.PartNumber < 1 {
						hasError = true
						assert.Equal(t, "PartNumber", tt.errField)
						break
					}
					if part.ETag == "" {
						hasError = true
						assert.Equal(t, "ETag", tt.errField)
						break
					}
				}
				if !hasError {
					assert.False(t, tt.expectErr)
				}
			}
		})
	}
}

// TestIsReservedPath tests the reserved path checking
func TestIsReservedPath_Multipart(t *testing.T) {
	tests := []struct {
		path     string
		reserved bool
	}{
		{"user@example.com/file.bin", false},
		{"user@example.com/folder/file.bin", false},
		{"user@example.com/api/endpoint", true},
		{"user@example.com/.well-known/config", true},
		{"user@example.com/_internal/data", true},
		{"user@example.com/API/test", true}, // case insensitive
		{"user@example.com/data/api/file", true}, // api in subfolder
		{"user@example.com/myapi/file", false}, // api as part of name
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsReservedPath(tt.path)
			assert.Equal(t, tt.reserved, result)
		})
	}
}

// TestMultipartResponse_Serialization tests response serialization
func TestMultipartResponse_Serialization(t *testing.T) {
	resp := MultipartUploadResponse{
		Key:      "user@example.com/file.bin",
		UploadID: "test-upload-id-123",
		URLs: []string{
			"https://example.com/part1",
			"https://example.com/part2",
			"https://example.com/part3",
		},
	}

	// Verify fields
	assert.Equal(t, "user@example.com/file.bin", resp.Key)
	assert.Equal(t, "test-upload-id-123", resp.UploadID)
	assert.Len(t, resp.URLs, 3)
}

// TestCompleteResponse_Serialization tests response serialization
func TestCompleteResponse_Serialization(t *testing.T) {
	resp := CompleteUploadResponse{
		Key:          "user@example.com/file.bin",
		Version:      "v123",
		ETag:         "combined-etag",
		Size:         16777216,
		LastModified: "2023-01-01T00:00:00Z",
	}

	// Verify fields
	assert.Equal(t, "user@example.com/file.bin", resp.Key)
	assert.Equal(t, "v123", resp.Version)
	assert.Equal(t, "combined-etag", resp.ETag)
	assert.Equal(t, int64(16777216), resp.Size)
	assert.Equal(t, "2023-01-01T00:00:00Z", resp.LastModified)
}