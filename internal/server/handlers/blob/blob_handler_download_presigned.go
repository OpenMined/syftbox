package blob

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/datasite"
)

func (h *BlobHandler) DownloadObjectsPresigned(ctx *gin.Context) {
	var req PresignUrlRequest

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.Error(fmt.Errorf("failed to bind json: %w", err))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	if len(req.Keys) == 0 {
		ctx.Error(fmt.Errorf("keys cannot be empty"))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "keys cannot be empty",
		})
		return
	}

	urls := make([]*BlobUrl, 0, len(req.Keys))
	errors := make([]*BlobError, 0)
	index := h.blob.Index()
	for _, key := range req.Keys {
		if !datasite.IsValidPath(key) {
			ctx.Error(fmt.Errorf("invalid datasite path: %s", key))
			errors = append(errors, &BlobError{
				Key:   key,
				Error: "invalid key",
			})
			continue
		}

		_, ok := index.Get(key)
		if !ok {
			ctx.Error(fmt.Errorf("object not found: %s", key))
			errors = append(errors, &BlobError{
				Key:   key,
				Error: "object not found",
			})
			continue
		}

		url, err := h.blob.Backend().GetObjectPresigned(ctx, key)
		if err != nil {
			ctx.Error(fmt.Errorf("failed to get object: %w", err))
			errors = append(errors, &BlobError{
				Key:   key,
				Error: err.Error(),
			})
			continue
		}
		urls = append(urls, &BlobUrl{
			Key: key,
			Url: url,
		})
	}

	code := http.StatusOK
	if len(urls) == 0 && len(errors) > 0 {
		code = http.StatusBadRequest
	} else if len(urls) > 0 && len(errors) > 0 {
		code = http.StatusMultiStatus
	}

	ctx.PureJSON(code, &PresignUrlResponse{
		URLs:   urls,
		Errors: errors,
	})
}
