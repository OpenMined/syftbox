package blob

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
)

func (h *BlobHandler) Upload(ctx *gin.Context) {
	var req UploadRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	if !isValidDatasiteKey(req.Key) {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "invalid key",
		})
		return
	}

	// get form file
	file, err := ctx.FormFile("file")
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid file: %s", err),
		})
		return
	}

	// check file size
	if file.Size <= 0 {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "invalid file: size is 0",
		})
		return
	}

	fd, err := file.Open()
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid file: %s", err),
		})
		return
	}

	defer fd.Close()
	result, err := h.svc.Client().PutObject(ctx.Request.Context(), &blob.PutObjectParams{
		Key:  req.Key,
		Size: file.Size,
		Body: fd,
	})
	if err != nil {
		slog.Error("failed to put object", "error", err)
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": "server error: could not persist file",
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
