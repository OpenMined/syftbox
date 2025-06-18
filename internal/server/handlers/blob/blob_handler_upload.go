package blob

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

func (h *BlobHandler) Upload(ctx *gin.Context) {
	user := ctx.GetString("user")

	if key := ctx.Query("key"); aclspec.IsACLFile(key) {
		h.UploadACL(ctx)
		return
	}

	var req UploadRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind query: %w", err))
		return
	}

	// todo check if new change using etag

	if !datasite.IsValidPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("invalid key: %s", req.Key))
		return
	}

	if err := h.checkPermissions(req.Key, user, acl.AccessWrite); err != nil {
		api.AbortWithError(ctx, http.StatusForbidden, api.CodeAccessDenied, err)
		return
	}

	// get form file
	file, err := ctx.FormFile("file")
	if err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid file: %w", err))
		return
	}

	// check file size
	if file.Size <= 0 {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid file: size is 0"))
		return
	}

	fd, err := file.Open()
	if err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid file file: %w", err))
		return
	}
	defer fd.Close()

	result, err := h.blob.Backend().PutObject(ctx.Request.Context(), &blob.PutObjectParams{
		Key:  req.Key,
		Size: file.Size,
		Body: fd,
	})
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobPutFailed, fmt.Errorf("failed to put object: %w", err))
		return
	}

	// response with UploadAccept
	ctx.PureJSON(http.StatusOK, &UploadResponse{
		Key:          result.Key,
		Version:      result.Version,
		ETag:         result.ETag,
		Size:         result.Size,
		LastModified: result.LastModified.Format(time.RFC3339),
	})
}
