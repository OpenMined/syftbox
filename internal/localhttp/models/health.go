package models

import "time"

// Health represents the health status of the service.
type Health struct {
	// Status is the health status ("healthy" or "unhealthy").
	Status string `json:"status"`
	// Timestamp is when the health check was performed.
	Timestamp time.Time `json:"timestamp"`
}
