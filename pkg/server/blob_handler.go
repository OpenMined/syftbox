package server

import (
	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/pkg/blob"
)

type uploadHandler struct {
	api *blob.BlobStorageAPI
	svc *blob.BlobStorageService
}

func NewBlobHandler(svc *blob.BlobStorageService) *uploadHandler {
	return &uploadHandler{
		svc: svc,
		api: svc.GetAPI(),
	}
}

type UploadRequest struct {
	blob.UploadRequest
}

type UploadAccept struct {
	blob.UploadResponse
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
	result, err := h.api.PresignedUpload(ctx, &req.UploadRequest)
	if err != nil {
		ctx.JSON(400, ErrorResponse{
			Code:    ErrUploadFailed,
			Message: err.Error(),
		})
		return
	}

	// response with UploadAccept
	ctx.JSON(200, result)
}

// ----------------------------

type CompleteRequest struct {
	blob.CompleteUploadRequest
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
		if err := h.api.CompleteUpload(ctx, &req.CompleteUploadRequest); err != nil {
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
	// res, err := h.api.ListObjects(ctx)
	// if err != nil {
	// 	ctx.JSON(400, ErrorResponse{
	// 		Code:    ErrListFailed,
	// 		Message: err.Error(),
	// 	})
	// 	return
	// }
	// if res == nil {
	// 	res = []*blob.BlobInfo{}
	// }
	res := h.svc.ListFiles()
	ctx.JSON(200, res)
}
