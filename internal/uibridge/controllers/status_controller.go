package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/uibridge/errors"
	"github.com/yashgorana/syftbox-go/internal/uibridge/services"
)

// StatusController handles service status requests.
type StatusController struct {
	statusService *services.StatusService
}

// NewStatusController creates a new status controller.
func NewStatusController(statusService *services.StatusService) *StatusController {
	return &StatusController{
		statusService: statusService,
	}
}

// RegisterRoutes registers the status endpoint with authentication.
func (c *StatusController) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/status", c.getStatus)
}

// getStatus returns the current service status.
// @Summary Get service status
// @Description Returns the current status of the service
// @Tags status
// @Produce json
// @Security APIToken
// @Success 200 {object} models.Status
// @Failure 401 {object} errors.ErrorResponse
// @Failure 500 {object} errors.ErrorResponse
// @Router /v1/status [get]
func (c *StatusController) getStatus(ctx *gin.Context) {
	status, err := c.statusService.GetStatus(ctx)
	if err != nil {
		appErr := errors.Internal("Failed to get service status", err)
		ctx.Error(appErr)
		return
	}

	ctx.JSON(http.StatusOK, status)
}
