package uibridge

import "time"

// Config contains configuration for the UI bridge server
type Config struct {
	// Enable the UI bridge server
	Enabled bool
	// Host to bind the UI bridge server
	Host string
	// Port for the UI bridge server (0 means random port)
	Port int
	// Access token for the UI bridge server
	Token string
	// EnableSwagger enables Swagger documentation
	EnableSwagger bool
	// RequestTimeout is the maximum duration for requests
	RequestTimeout time.Duration
	// RateLimit is requests per second per client IP
	RateLimit float64
	// RateLimitBurst is the maximum burst size for rate limiting
	RateLimitBurst int
}
