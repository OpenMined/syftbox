package acl

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/handlers/api"
)

type ACLHandler struct {
	aclSvc *acl.ACLService
}

func NewACLHandler(svc *acl.ACLService) *ACLHandler {
	return &ACLHandler{
		aclSvc: svc,
	}
}

func (h *ACLHandler) CheckAccess(ctx *gin.Context) {
	var req ACLCheckRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, err)
		return
	}

	// Check access using the ACL service
	if err := h.aclSvc.CanAccess(
		&acl.User{ID: req.User},
		&acl.File{Path: req.Path, Size: req.Size},
		acl.AccessLevel(req.Level),
	); err != nil {
		api.AbortWithError(ctx, http.StatusForbidden, api.CodeAccessDenied, err)
		return
	}

	ctx.PureJSON(http.StatusOK, &ACLCheckResponse{
		User:  req.User,
		Path:  req.Path,
		Level: req.Level.String(),
	})
}
