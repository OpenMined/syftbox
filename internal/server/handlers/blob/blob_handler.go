package blob

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

type BlobHandler struct {
	blob *blob.BlobService
	acl  *acl.ACLService
}

func New(blob *blob.BlobService, acl *acl.ACLService) *BlobHandler {
	return &BlobHandler{blob: blob, acl: acl}
}

func (h *BlobHandler) UploadMultipart(ctx *gin.Context) {
	// todo
	api.AbortWithError(ctx, http.StatusNotImplemented, api.CodeInvalidRequest, fmt.Errorf("not implemented"))
}

func (h *BlobHandler) UploadComplete(ctx *gin.Context) {
	// todo
	api.AbortWithError(ctx, http.StatusNotImplemented, api.CodeInvalidRequest, fmt.Errorf("not implemented"))
}

func (h *BlobHandler) ListObjects(ctx *gin.Context) {
	res, err := h.blob.Index().List()
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobListFailed, err)
		return
	}

	ctx.PureJSON(http.StatusOK, &gin.H{
		"blobs": res,
	})
}

func (h *BlobHandler) checkPermissions(key string, user string, access acl.AccessLevel) error {
	if datasite.IsOwner(key, user) {
		return nil
	}

	if err := h.acl.CanAccess(&acl.User{ID: user}, &acl.File{Path: key}, access); err != nil {
		return err
	}

	return nil
}

// IsReservedPath checks if a path contains reserved system paths
func IsReservedPath(path string) bool {
	// Clean the path
	path = datasite.CleanPath(path)
	
	// Extract the part after the datasite name
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return false
	}
	
	// Skip the datasite name (email) and check subsequent parts
	for i := 1; i < len(parts); i++ {
		part := strings.ToLower(parts[i])
		// Check for reserved paths
		if part == "api" || strings.HasPrefix(part, "api/") ||
		   part == ".well-known" || strings.HasPrefix(part, ".well-known/") ||
		   part == "_internal" || strings.HasPrefix(part, "_internal/") {
			return true
		}
	}
	
	return false
}
