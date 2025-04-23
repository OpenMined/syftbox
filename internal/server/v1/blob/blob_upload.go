package blob

import (
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

	// get form file
	file, err := ctx.FormFile("file")
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// check file size
	if file.Size <= 0 {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "invalid content length",
		})
		return
	}

	fd, err := file.Open()
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
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
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
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
