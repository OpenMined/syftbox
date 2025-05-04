package blob

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *BlobHandler) UploadPresigned(ctx *gin.Context) {
	var req PresignUrlRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	if len(req.Keys) == 0 {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "keys cannot be empty",
		})
		return
	}

	urls := make([]*BlobUrl, 0, len(req.Keys))
	errors := make([]*BlobError, 0)
	for _, key := range req.Keys {
		if !isValidDatasiteKey(key) {
			errors = append(errors, &BlobError{
				Key:   key,
				Error: "invalid key",
			})
			continue
		}
		url, err := h.blob.Backend().PutObjectPresigned(ctx, key)
		if err != nil {
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
