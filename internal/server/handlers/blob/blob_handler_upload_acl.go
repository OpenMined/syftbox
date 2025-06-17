package blob

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

func (h *BlobHandler) UploadACL(ctx *gin.Context) {
	var req UploadRequest
	user := ctx.GetString("user")

	if err := ctx.ShouldBindQuery(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind query: %w", err))
		return
	}

	if !(datasite.IsValidPath(req.Key) && aclspec.IsACLFile(req.Key)) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("invalid ruleset path: %s", req.Key))
		return
	}

	// check if user has admin rights
	if err := h.checkPermissions(req.Key, user, acl.AccessAdmin); err != nil {
		api.AbortWithError(ctx, http.StatusForbidden, api.CodeAccessDenied, err)
		return
	}

	// get form file
	file, err := ctx.FormFile("file")
	if err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to get form file: %w", err))
		return
	}

	// check file size
	if file.Size <= 0 {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid file: size is 0"))
		return
	}

	// open the file
	fd, err := file.Open()
	if err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to open file: %w", err))
		return
	}
	defer fd.Close()

	// read the file into memory, because we need to read it twice
	fdBytes, err := io.ReadAll(fd)
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeInvalidRequest, fmt.Errorf("failed to read file: %w", err))
		return
	}
	aclBytesReader := bytes.NewReader(fdBytes)

	// load aclspec
	ruleset, err := aclspec.LoadFromReader(req.Key, aclBytesReader)
	if err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to read ruleset: %w", err))
	}

	// upload file to s3
	// because that's always the ground truth
	blobBytesReader := bytes.NewReader(fdBytes)
	result, err := h.blob.Backend().PutObject(ctx.Request.Context(), &blob.PutObjectParams{
		Key:  req.Key,
		Size: file.Size,
		Body: blobBytesReader,
	})
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobPutFailed, fmt.Errorf("failed to put object: %w", err))
		return
	}

	// add it to the acl service
	if _, err := h.acl.AddRuleSet(ruleset); err != nil {
		// if this error happens, there's a pretty serious bug in the acl service
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeACLUpdateFailed, fmt.Errorf("failed to update ruleset: %w", err))
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
