package errors

// ErrorResponse represents a standardized error response for the API
// @Description Standardized error response structure
type ErrorResponse struct {
	// Error code identifier
	Code string `json:"code" example:"unauthorized"`
	// Human-readable error message
	Message string `json:"message" example:"Authentication required"`
	// Optional additional error details
	Details map[string]interface{} `json:"details,omitempty"`
}
