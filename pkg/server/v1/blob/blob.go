package blob

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/pkg/blob"
)

const (
	errPresignFailed  = "BLOB_UPLOAD_PRESIGN_FAILED"
	errCompleteFailed = "BLOB_COMPLETE_FAILED"
)

type UploadHandler struct {
	c   *blob.BlobClient
	svc *blob.BlobService
}

func NewHandler(svc *blob.BlobService) *UploadHandler {
	return &UploadHandler{
		svc: svc,
		c:   svc.GetClient(),
	}
}

func (h *UploadHandler) Upload(ctx *gin.Context) {
	var req UploadRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// todo - validate the request
	result, err := h.c.PresignedUpload(ctx, &req.UploadRequest)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"code":  errPresignFailed,
			"error": err.Error(),
		})
		return
	}

	// response with UploadAccept
	ctx.PureJSON(http.StatusOK, result)
}

func (h *UploadHandler) Download(ctx *gin.Context) {
	key := ctx.Query("key")
	if key == "" {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "key is required",
		})
		return
	}

	// todo - validate the request
	result, err := h.c.PresignedDownload(ctx, key)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.PureJSON(http.StatusOK, gin.H{
		"url": result,
	})
}

func (h *UploadHandler) Complete(ctx *gin.Context) {
	var req CompleteRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// todo - complete the request
	// 1. no such request
	// 2. bad request
	// 3. request expired
	// 4. request already completed

	// completing a multi-part upload
	if req.UploadId != "" && req.Parts != nil {
		if err := h.c.CompleteUpload(ctx, &req.CompleteUploadRequest); err != nil {
			ctx.PureJSON(http.StatusInternalServerError, gin.H{
				"code":  errCompleteFailed,
				"error": err.Error(),
			})
			return
		}
	}

	ctx.PureJSON(http.StatusOK, req)
}

func (h *UploadHandler) List(ctx *gin.Context) {
	res := h.svc.List()
	ctx.PureJSON(http.StatusOK, res)
}
