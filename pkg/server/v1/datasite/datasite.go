package datasite

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/pkg/datasite"
)

type DatasiteHandler struct {
	svc *datasite.DatasiteService
}

func NewHandler(svc *datasite.DatasiteService) *DatasiteHandler {
	return &DatasiteHandler{
		svc: svc,
	}
}

func (h *DatasiteHandler) GetView(ctx *gin.Context) {
	user, _ := ctx.GetQuery("user")

	if user == "" {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "query param 'user' is required",
		})
		return
	}

	view := h.svc.GetView(user)
	ctx.PureJSON(http.StatusOK, view)
}
