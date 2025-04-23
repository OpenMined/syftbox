package blob

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
)

type BlobHandler struct {
	svc *blob.BlobService
}

func New(svc *blob.BlobService) *BlobHandler {
	return &BlobHandler{svc: svc}
}

func (h *BlobHandler) UploadMultipart(ctx *gin.Context) {
	// todo
	ctx.PureJSON(http.StatusNotImplemented, gin.H{
		"error": "not implemented",
	})
}

func (h *BlobHandler) UploadComplete(ctx *gin.Context) {
	// todo
	ctx.PureJSON(http.StatusNotImplemented, gin.H{
		"error": "not implemented",
	})
}

func (h *BlobHandler) ListObjects(ctx *gin.Context) {
	res, err := h.svc.Index().List()
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.PureJSON(http.StatusOK, res)
}
