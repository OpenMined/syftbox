package server

import (
	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/pkg/datasite"
)

type DatasiteHandler struct {
	svc *datasite.DatasiteService
}

func NewDatasiteHandler(svc *datasite.DatasiteService) *DatasiteHandler {
	return &DatasiteHandler{
		svc: svc,
	}
}

func (h *DatasiteHandler) GetView(ctx *gin.Context) {
	user, _ := ctx.GetQuery("user")

	if user == "" {
		ctx.JSON(400, ErrorResponse{
			Code:    ErrBadRequest,
			Message: "query param 'user' is required",
		})
		return
	}

	view := h.svc.GetView(user)
	ctx.JSON(200, view)
}
