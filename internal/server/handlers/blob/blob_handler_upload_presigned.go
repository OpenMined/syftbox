package blob

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

func (h *BlobHandler) UploadPresigned(ctx *gin.Context) {
	var req PresignURLRequest
	user := ctx.GetString("user")

	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	urls := make([]*BlobURL, 0, len(req.Keys))
	errors := make([]*BlobAPIError, 0)
	for _, key := range req.Keys {
		if !datasite.IsValidPath(key) {
			errors = append(errors, &BlobAPIError{
				SyftAPIError: api.SyftAPIError{
					Code:    api.CodeDatasiteInvalidPath,
					Message: "invalid key",
				},
				Key: key,
			})
			continue
		}

		if err := h.checkPermissions(key, user, acl.AccessWrite); err != nil {
			errors = append(errors, &BlobAPIError{
				SyftAPIError: api.SyftAPIError{
					Code:    api.CodeAccessDenied,
					Message: err.Error(),
				},
				Key: key,
			})
			continue
		}

		url, err := h.blob.Backend().PutObjectPresigned(ctx, key)
		if err != nil {
			errors = append(errors, &BlobAPIError{
				SyftAPIError: api.SyftAPIError{
					Code:    api.CodeBlobPutFailed,
					Message: err.Error(),
				},
				Key: key,
			})
			continue
		}
		urls = append(urls, &BlobURL{
			Key: key,
			Url: url,
		})
	}

	code := http.StatusOK
	if len(urls) == 0 && len(errors) > 0 {
		code = http.StatusMultiStatus
	} else if len(urls) > 0 && len(errors) > 0 {
		code = http.StatusMultiStatus
	}

	ctx.PureJSON(code, &PresignURLResponse{
		URLs:   urls,
		Errors: errors,
	})
}
