package blob

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
)

var (
	regexDatasiteKey = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+/`)
)

type BlobHandler struct {
	blob *blob.BlobService
}

func New(blob *blob.BlobService) *BlobHandler {
	return &BlobHandler{blob: blob}
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
	res, err := h.blob.Index().List()
	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.PureJSON(http.StatusOK, &gin.H{
		"blobs": res,
	})
}

func isValidDatasiteKey(key string) bool {
	return regexDatasiteKey.MatchString(key)
}
