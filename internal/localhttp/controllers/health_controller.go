package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/localhttp/errors"
	"github.com/yashgorana/syftbox-go/internal/localhttp/services"
)

// HealthController handles health check requests.
type HealthController struct {
	healthService *services.HealthService
}

// NewHealthController creates a new health controller.
func NewHealthController(healthService *services.HealthService) *HealthController {
	return &HealthController{
		healthService: healthService,
	}
}

// RegisterRoutes registers the health endpoint.
func (c *HealthController) RegisterRoutes(router *gin.Engine) {
	router.GET("/health", c.getHealth)
}

// getHealth returns the service health status.
// @Summary Get health status
// @Description Returns the health status of the service
// @Tags health
// @Produce json
// @Success 200 {object} models.Health
// @Failure 503 {object} errors.ErrorResponse
// @Router /health [get]
func (c *HealthController) getHealth(ctx *gin.Context) {
	health, err := c.healthService.GetHealth(ctx)
	if err != nil {
		appErr := errors.Internal("Failed to check health", err)
		ctx.Error(appErr)
		return
	}

	// Set appropriate status code based on health
	statusCode := http.StatusOK
	if health.Status != "healthy" {
		statusCode = http.StatusServiceUnavailable
	}

	ctx.JSON(statusCode, health)
}
