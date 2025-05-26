package blob

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
)

func (h *BlobHandler) Upload(ctx *gin.Context) {
	var req UploadRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		ctx.Error(fmt.Errorf("failed to bind query: %w", err))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// todo check if new change using etag

	if !datasite.IsValidPath(req.Key) {
		ctx.Error(fmt.Errorf("invalid datasite path: %s", req.Key))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid key: %s", req.Key),
		})
		return
	}

	// get form file
	file, err := ctx.FormFile("file")
	if err != nil {
		ctx.Error(fmt.Errorf("failed to get form file: %w", err))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid file: %s", err),
		})
		return
	}

	// check file size
	if file.Size <= 0 {
		ctx.Error(fmt.Errorf("invalid file: size is 0"))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "invalid file: size is 0",
		})
		return
	}

	fd, err := file.Open()
	if err != nil {
		ctx.Error(fmt.Errorf("failed to open file: %w", err))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid file: %s", err),
		})
		return
	}
	defer fd.Close()

	result, err := h.blob.Backend().PutObject(ctx.Request.Context(), &blob.PutObjectParams{
		Key:  req.Key,
		Size: file.Size,
		Body: fd,
	})
	if err != nil {
		ctx.Error(fmt.Errorf("failed to put object: %w", err))
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to put object: %s", err),
		})
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
