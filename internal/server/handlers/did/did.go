package did

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/utils"
)

type DIDHandler struct {
	blobService *blob.BlobService
}

func NewDIDHandler(blobService *blob.BlobService) *DIDHandler {
	return &DIDHandler{blobService: blobService}
}

func (h *DIDHandler) GetDID(c *gin.Context) {
	user := c.Param("user")

	// check if user is a valid email
	if !utils.IsValidEmail(user) {
		api.ServeErrorHTML(c, http.StatusBadRequest, "Invalid Request", "<b><code>"+user+"</code></b> is not a valid user.")
		return
	}

	didKey := fmt.Sprintf("%s/public/did.json", user)

	// check if it exists in blob index
	_, exists := h.blobService.Index().Get(didKey)
	if !exists {
		api.Serve404HTML(c)
		return
	}

	resp, err := h.blobService.Backend().GetObject(c.Request.Context(), didKey)
	if err != nil {
		c.Error(err)
		api.Serve500HTML(c, err)
		return
	}
	defer resp.Body.Close()

	// resp.ContentType may not have the correct MIME type
	contentType := utils.DetectContentType(didKey)
	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)

	// Stream response body directly
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		c.Error(err)
		api.Serve500HTML(c, fmt.Errorf("failed to read file: %w", err))
		return
	}
}
