package blob

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
)

const (
	defaultMultipartPartSize = int64(64 * 1024 * 1024) // 64MB keeps part count low for large files
	minMultipartPartSize     = int64(5 * 1024 * 1024)  // S3/MinIO minimum
	maxMultipartParts        = 10000
)

type BlobHandler struct {
	blob *blob.BlobService
	acl  *acl.ACLService
	hub  *ws.WebsocketHub
}

func New(blob *blob.BlobService, acl *acl.ACLService, hub *ws.WebsocketHub) *BlobHandler {
	return &BlobHandler{blob: blob, acl: acl, hub: hub}
}

func (h *BlobHandler) notifyFileUploaded(path string, etag string, size int64) {
	if h.hub == nil {
		return
	}

	fileNotify := syftmsg.FileWrite{
		Path:   path,
		ETag:   etag,
		Length: size,
	}

	msg := &syftmsg.Message{
		Id:   "srv",
		Type: syftmsg.MsgFileNotify,
		Data: fileNotify,
	}

	h.hub.BroadcastFiltered(msg, func(info *ws.ClientInfo) bool {
		return info.Version != ""
	})
	slog.Debug("broadcasted file notify to new clients", "path", path, "etag", etag, "size", size)
}

func (h *BlobHandler) UploadMultipart(ctx *gin.Context) {
	user := ctx.GetString("user")

	var req MultipartUploadRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	if req.Size <= 0 {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid size"))
		return
	}

	if !datasite.IsValidPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("invalid key: %s", req.Key))
		return
	}

	if IsReservedPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("reserved path: %s", req.Key))
		return
	}

	if err := h.checkPermissions(req.Key, user, acl.AccessWrite); err != nil {
		api.AbortWithError(ctx, http.StatusForbidden, api.CodeAccessDenied, err)
		return
	}

	partSize := req.PartSize
	if partSize <= 0 {
		partSize = defaultMultipartPartSize
	}
	if partSize < minMultipartPartSize {
		partSize = minMultipartPartSize
	}

	totalParts := int(math.Ceil(float64(req.Size) / float64(partSize)))
	if totalParts <= 0 || totalParts > maxMultipartParts {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid part count: %d", totalParts))
		return
	}

	partNumbers := req.PartNumbers
	if len(partNumbers) == 0 {
		partNumbers = make([]int, 0, totalParts)
		for i := 1; i <= totalParts; i++ {
			partNumbers = append(partNumbers, i)
		}
	}

	// Ensure part numbers are unique and within bounds
	seen := make(map[int]struct{}, len(partNumbers))
	filtered := make([]int, 0, len(partNumbers))
	for _, pn := range partNumbers {
		if pn <= 0 || pn > totalParts {
			api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid part number: %d", pn))
			return
		}
		if _, ok := seen[pn]; ok {
			continue
		}
		seen[pn] = struct{}{}
		filtered = append(filtered, pn)
	}
	sort.Ints(filtered)

	result, err := h.blob.Backend().PutObjectMultipart(ctx.Request.Context(), &blob.PutObjectMultipartParams{
		Key:          req.Key,
		Parts:        uint16(totalParts),
		UploadID:     req.UploadID,
		PartNumbers:  filtered,
		PartSizeHint: partSize,
	})
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobPutFailed, fmt.Errorf("failed to start multipart upload: %w", err))
		return
	}

	ctx.PureJSON(http.StatusOK, &MultipartUploadResponse{
		Key:       result.Key,
		UploadID:  result.UploadID,
		PartSize:  partSize,
		URLs:      result.URLs,
		PartCount: totalParts,
	})
}

func (h *BlobHandler) UploadComplete(ctx *gin.Context) {
	user := ctx.GetString("user")

	var req CompleteMultipartUploadRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	if req.UploadID == "" || len(req.Parts) == 0 {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("missing upload parts"))
		return
	}

	if !datasite.IsValidPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("invalid key: %s", req.Key))
		return
	}

	if IsReservedPath(req.Key) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeDatasiteInvalidPath, fmt.Errorf("reserved path: %s", req.Key))
		return
	}

	if err := h.checkPermissions(req.Key, user, acl.AccessWrite); err != nil {
		api.AbortWithError(ctx, http.StatusForbidden, api.CodeAccessDenied, err)
		return
	}

	result, err := h.blob.Backend().CompleteMultipartUpload(ctx.Request.Context(), &blob.CompleteMultipartUploadParams{
		Key:      req.Key,
		UploadID: req.UploadID,
		Parts:    req.Parts,
	})
	if err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeBlobPutFailed, fmt.Errorf("failed to complete multipart upload: %w", err))
		return
	}

	h.notifyFileUploaded(result.Key, result.ETag, result.Size)

	ctx.PureJSON(http.StatusOK, &UploadResponse{
		Key:          result.Key,
		Version:      result.Version,
		ETag:         result.ETag,
		Size:         result.Size,
		LastModified: result.LastModified.Format(time.RFC3339),
	})
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

	if err := h.acl.CanAccess(acl.NewRequest(key, &acl.User{ID: user}, access)); err != nil {
		return err
	}

	return nil
}

// IsReservedPath checks if a path contains reserved system paths
func IsReservedPath(path string) bool {
	// Clean the path
	path = datasite.CleanPath(path)

	// Extract the part after the datasite name
	parts := strings.Split(path, datasite.PathSep)
	if len(parts) < 2 {
		return false
	}

	// Skip the datasite name (email) and check subsequent parts
	for i := 1; i < len(parts); i++ {
		part := strings.ToLower(parts[i])
		// Check for reserved paths
		if part == "api" || part == ".well-known" || part == "_internal" {
			return true
		}
	}

	return false
}
