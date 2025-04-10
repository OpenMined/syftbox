package models

import "time"

// Status represents the status of the service.
type Status struct {
	// Status indicates whether the service is online or offline.
	Status string `json:"status"`
	// Timestamp is when the status was checked.
	Timestamp time.Time `json:"timestamp"`
}
