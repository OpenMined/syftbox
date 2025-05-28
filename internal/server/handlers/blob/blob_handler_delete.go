package blob

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/aclspec"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

func (h *BlobHandler) DeleteObjects(ctx *gin.Context) {
	var req DeleteRequest
	user := ctx.GetString("user")

	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	deleted := make([]string, 0, len(req.Keys))
	errors := make([]*BlobAPIError, 0)
	for _, key := range req.Keys {
		if !datasite.IsValidPath(key) {
			errors = append(errors, NewBlobAPIError(api.CodeDatasiteInvalidPath, "invalid key", key))
			continue
		}

		if err := h.checkPermissions(key, user, acl.AccessWrite); err != nil {
			errors = append(errors, NewBlobAPIError(api.CodeAccessDenied, err.Error(), key))
			continue
		}

		_, err := h.blob.Backend().DeleteObject(ctx.Request.Context(), key)
		if err != nil {
			ctx.Error(fmt.Errorf("failed to delete object: %w", err))
			errors = append(errors, NewBlobAPIError(api.CodeBlobDeleteFailed, err.Error(), key))
			continue
		}

		if aclspec.IsACLFile(key) {
			// don't worry the above permissions check will make sure that the user is admin
			ok := h.acl.RemoveRuleSet(key)
			if !ok {
				slog.Warn("remove ruleset returned false", "key", key)
			}
		}

		deleted = append(deleted, key)
	}

	code := http.StatusOK
	if len(deleted) >= 0 && len(errors) >= 0 {
		code = http.StatusMultiStatus
	}

	ctx.PureJSON(code, &DeleteResponse{
		Deleted: deleted,
		Errors:  errors,
	})
}
