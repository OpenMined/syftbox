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