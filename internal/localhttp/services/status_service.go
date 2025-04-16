package services

import (
	"context"
	"time"

	"github.com/yashgorana/syftbox-go/internal/localhttp/models"
)

// StatusService provides service status information.
type StatusService struct{}

// NewStatusService creates a new status service.
func NewStatusService() *StatusService {
	return &StatusService{}
}

// GetStatus returns the current service status.
func (s *StatusService) GetStatus(ctx context.Context) (*models.Status, error) {
	return &models.Status{
		Status:    "online",
		Timestamp: time.Now(),
	}, nil
}
