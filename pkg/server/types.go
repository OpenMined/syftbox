package server

// Create consistent error types
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
