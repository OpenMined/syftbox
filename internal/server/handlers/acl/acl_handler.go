package acl

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
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
		ctx.Error(err)
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Check access using the ACL service
	if err := h.aclSvc.CanAccess(
		&acl.User{ID: req.User},
		&acl.File{Path: req.Path, Size: req.Size},
		acl.AccessLevel(req.Level),
	); err != nil {
		ctx.Error(err)
		ctx.PureJSON(http.StatusForbidden, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.PureJSON(http.StatusOK, &ACLCheckResponse{
		User:  req.User,
		Path:  req.Path,
		Level: req.Level.String(),
	})
}
