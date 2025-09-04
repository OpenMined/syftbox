package blob

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/accesslog"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

func (h *BlobHandler) DownloadObjectsPresigned(ctx *gin.Context) {
	var req PresignURLRequest
	user := ctx.GetString("user")

	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	urls := make([]*BlobURL, 0, len(req.Keys))
	errors := make([]*BlobAPIError, 0)
	index := h.blob.Index()
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

		if err := h.checkPermissions(key, user, acl.AccessRead); err != nil {
			if logger := accesslog.GetAccessLogger(ctx); logger != nil {
				logger.LogAccess(ctx, key, accesslog.AccessTypeRead, acl.AccessRead, false, err.Error())
			}
			errors = append(errors, &BlobAPIError{
				SyftAPIError: api.SyftAPIError{
					Code:    api.CodeAccessDenied,
					Message: err.Error(),
				},
				Key: key,
			})
			continue
		}
		
		if logger := accesslog.GetAccessLogger(ctx); logger != nil {
			logger.LogAccess(ctx, key, accesslog.AccessTypeRead, acl.AccessRead, true, "")
		}

		_, ok := index.Get(key)
		if !ok {
			errors = append(errors, &BlobAPIError{
				SyftAPIError: api.SyftAPIError{
					Code:    api.CodeBlobNotFound,
					Message: "object not found",
				},
				Key: key,
			})
			continue
		}

		url, err := h.blob.Backend().GetObjectPresigned(ctx, key)
		if err != nil {
			errors = append(errors, &BlobAPIError{
				SyftAPIError: api.SyftAPIError{
					Code:    api.CodeBlobGetFailed,
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
		code = http.StatusBadRequest
	} else if len(urls) > 0 && len(errors) > 0 {
		code = http.StatusMultiStatus
	}

	ctx.PureJSON(code, &PresignURLResponse{
		URLs:   urls,
		Errors: errors,
	})
}
