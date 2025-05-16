package blob

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *BlobHandler) DeleteObjects(ctx *gin.Context) {
	var req DeleteRequest
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

	deleted := make([]string, 0, len(req.Keys))
	errors := make([]*BlobError, 0)
	for _, key := range req.Keys {
		if !isValidDatasiteKey(key) {
			ctx.Error(fmt.Errorf("invalid datasite path: %s", key))
			errors = append(errors, &BlobError{
				Key:   key,
				Error: "invalid key",
			})
			continue
		}
		_, err := h.blob.Backend().DeleteObject(ctx.Request.Context(), key)
		if err != nil {
			ctx.Error(fmt.Errorf("failed to delete object: %w", err))
			errors = append(errors, &BlobError{
				Key:   key,
				Error: err.Error(),
			})
			continue
		}
		deleted = append(deleted, key)
	}

	code := http.StatusOK
	if len(deleted) == 0 && len(errors) > 0 {
		code = http.StatusBadRequest
	} else if len(deleted) > 0 && len(errors) > 0 {
		code = http.StatusMultiStatus
	}

	ctx.PureJSON(code, &DeleteResponse{
		Deleted: deleted,
		Errors:  errors,
	})
}
