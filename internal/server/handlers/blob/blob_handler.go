package blob

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

type BlobHandler struct {
	blob *blob.BlobService
	acl  *acl.ACLService
}

func New(blob *blob.BlobService, acl *acl.ACLService) *BlobHandler {
	return &BlobHandler{blob: blob, acl: acl}
}

func (h *BlobHandler) ListObjects(ctx *gin.Context) {
	res, err := h.blob.Index().List()
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobListFailed, err)
		return
	}

	ctx.PureJSON(http.StatusOK, &gin.H{
		"blobs": res,
	})
}

func (h *BlobHandler) checkPermissions(key string, user string, access acl.AccessLevel) error {
	if datasite.IsOwner(key, user) {
		return nil
	}

	if err := h.acl.CanAccess(acl.NewRequest(key, &acl.User{ID: user}, access)); err != nil {
		return err
	}

	return nil
}

// IsReservedPath checks if a path contains reserved system paths
func IsReservedPath(path string) bool {
	// Clean the path
	path = datasite.CleanPath(path)

	// Extract the part after the datasite name
	parts := strings.Split(path, datasite.PathSep)
	if len(parts) < 2 {
		return false
	}

	// Skip the datasite name (email) and check subsequent parts
	for i := 1; i < len(parts); i++ {
		part := strings.ToLower(parts[i])
		// Check for reserved paths
		if part == "api" || part == ".well-known" || part == "_internal" {
			return true
		}
	}

	return false
}

// UploadMultipart initiates a multipart upload and returns presigned URLs for each part
func (h *BlobHandler) UploadMultipart(ctx *gin.Context) {
	var req MultipartUploadRequest
	user := ctx.GetString("user")

	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	// Validate key format
	if !datasite.IsValidPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("invalid key"))
		return
	}

	// Check for reserved paths
	if IsReservedPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("reserved path"))
		return
	}

	// Check permissions
	if err := h.checkPermissions(req.Key, user, acl.AccessWrite); err != nil {
		api.AbortWithError(ctx, http.StatusForbidden, api.CodeAccessDenied, err)
		return
	}

	// Create multipart upload parameters
	params := &blob.PutObjectMultipartParams{
		Key:   req.Key,
		Parts: uint16(req.Parts),
	}

	// Initiate multipart upload
	resp, err := h.blob.Backend().PutObjectMultipart(ctx, params)
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobPutFailed, fmt.Errorf("failed to initiate multipart upload: %w", err))
		return
	}

	// Return response
	ctx.JSON(http.StatusOK, &MultipartUploadResponse{
		Key:      resp.Key,
		UploadID: resp.UploadID,
		URLs:     resp.URLs,
	})
}

// UploadComplete completes a multipart upload
func (h *BlobHandler) UploadComplete(ctx *gin.Context) {
	var req CompleteUploadRequest
	user := ctx.GetString("user")

	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	// Validate key format
	if !datasite.IsValidPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("invalid key"))
		return
	}

	// Check for reserved paths
	if IsReservedPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("reserved path"))
		return
	}

	// Check permissions
	if err := h.checkPermissions(req.Key, user, acl.AccessWrite); err != nil {
		api.AbortWithError(ctx, http.StatusForbidden, api.CodeAccessDenied, err)
		return
	}

	// Convert request parts to backend format
	parts := make([]*blob.CompletedPart, len(req.Parts))
	for i, part := range req.Parts {
		parts[i] = &blob.CompletedPart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		}
	}

	// Create complete multipart upload parameters
	params := &blob.CompleteMultipartUploadParams{
		Key:      req.Key,
		UploadID: req.UploadID,
		Parts:    parts,
	}

	// Complete multipart upload
	resp, err := h.blob.Backend().CompleteMultipartUpload(ctx, params)
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobPutFailed, fmt.Errorf("failed to complete multipart upload: %w", err))
		return
	}

	// Return response
	ctx.JSON(http.StatusOK, &CompleteUploadResponse{
		Key:          resp.Key,
		Version:      resp.Version,
		ETag:         resp.ETag,
		Size:         resp.Size,
		LastModified: resp.LastModified.Format("2006-01-02T15:04:05Z"),
	})
}
