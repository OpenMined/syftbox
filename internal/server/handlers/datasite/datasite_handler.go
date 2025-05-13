package datasite

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/datasite"
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

	if user == "" {
		ctx.Error(fmt.Errorf("`user` is required"))
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "user is required",
		})
		return
	}

	ctx.PureJSON(http.StatusOK, gin.H{
		"files": h.svc.GetView(user),
	})
}
