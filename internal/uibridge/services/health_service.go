package services

import (
	"context"
	"time"

	"github.com/yashgorana/syftbox-go/internal/uibridge/models"
)

// HealthService provides health status information.
type HealthService struct{}

// NewHealthService creates a new health service.
func NewHealthService() *HealthService {
	return &HealthService{}
}

// GetHealth returns the current health status.
func (s *HealthService) GetHealth(ctx context.Context) (*models.Health, error) {
	// For now, we'll always return healthy
	// In a real implementation, this could check dependencies, etc.
	return &models.Health{
		Status:    "healthy",
		Timestamp: time.Now(),
	}, nil
}
