package blob

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

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