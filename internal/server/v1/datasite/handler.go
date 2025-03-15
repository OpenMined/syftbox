package datasite

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/datasite"
)

type DatasiteHandler struct {
	svc *datasite.DatasiteService
}

func New(svc *datasite.DatasiteService) *DatasiteHandler {
	return &DatasiteHandler{
		svc: svc,
	}
}

func (h *DatasiteHandler) GetView(ctx *gin.Context) {
	user := ctx.GetString("user")

	ctx.PureJSON(http.StatusOK, gin.H{
		"files": h.svc.GetView(user),
	})
}

func (h *DatasiteHandler) DownloadFiles(ctx *gin.Context) {
	user := ctx.GetString("user")

	var dlReq DownloadRequest
	if err := ctx.ShouldBindJSON(&dlReq); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	blobUrls, blobErrors, err := h.svc.DownloadFiles(ctx.Request.Context(), user, dlReq.Keys)
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	code := http.StatusOK
	if len(blobErrors) > 0 {
		code = http.StatusMultiStatus
	}

	ctx.PureJSON(code, DownloadResponse{
		URLs:   blobUrls,
		Errors: blobErrors,
	})
}
