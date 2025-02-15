package server

import (
	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/pkg/blob"
)

const (
	ErrBadRequest     = "ERR_BAD_REQUEST"
	ErrUploadFailed   = "ERR_UPLOAD_FAILED"
	ErrCompleteFailed = "ERR_COMPLETE"
	ErrListFailed     = "ERR_LIST_FAILED"
)

type uploadHandler struct {
	blob *blob.BlobService
}

func NewBlobHandler(blob *blob.BlobService) *uploadHandler {
	return &uploadHandler{blob}
}

type UploadRequest struct {
	blob.FileUploadInput
}

type UploadAccept struct {
	blob.FileUploadOutput
}

// Create consistent error types
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *uploadHandler) Upload(ctx *gin.Context) {
	var req UploadRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		ctx.JSON(400, ErrorResponse{
			Code:    ErrBadRequest,
			Message: err.Error(),
		})
		return
	}

	// todo - validate the request
	result, err := h.blob.PresignedUpload(ctx, &req.FileUploadInput)
	if err != nil {
		ctx.JSON(400, ErrorResponse{
			Code:    ErrUploadFailed,
			Message: err.Error(),
		})
		return
	}

	// responsd with UploadAccept
	ctx.JSON(200, result)
}

// ----------------------------

type CompleteRequest struct {
	blob.CompleteUploadInput
}

func (h *uploadHandler) Complete(ctx *gin.Context) {
	var req CompleteRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(400, ErrorResponse{
			Code:    ErrBadRequest,
			Message: err.Error(),
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
		if err := h.blob.CompleteUpload(ctx, &req.CompleteUploadInput); err != nil {
			ctx.JSON(400, ErrorResponse{
				Code:    ErrCompleteFailed,
				Message: err.Error(),
			})
			return
		}
	}

	ctx.JSON(200, gin.H{"status": "ok"})
}

func (h *uploadHandler) List(ctx *gin.Context) {
	res, err := h.blob.ListObjects(ctx)
	if err != nil {
		ctx.JSON(400, ErrorResponse{
			Code:    ErrListFailed,
			Message: err.Error(),
		})
		return
	}
	if res == nil {
		res = []*blob.BlobInfo{}
	}
	ctx.JSON(200, res)
}
