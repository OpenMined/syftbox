package blob

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *BlobHandler) DeleteObjects(ctx *gin.Context) {
	var req DeleteRequest
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

	deleted := make([]string, 0, len(req.Keys))
	errors := make([]*BlobError, 0)
	for _, key := range req.Keys {
		_, err := h.svc.Client().DeleteObject(ctx.Request.Context(), key)
		if err != nil {
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
