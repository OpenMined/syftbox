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
