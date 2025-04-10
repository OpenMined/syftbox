package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError represents an application error with metadata
type AppError struct {
	// Code is the error code
	Code string `json:"code"`
	// Message is the human-readable error message
	Message string `json:"message"`
	// Status is the HTTP status code
	Status int `json:"-"`
	// Internal is the internal error that caused this error
	Internal error `json:"-"`
	// Details contains additional error details
	Details map[string]interface{} `json:"details,omitempty"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Internal != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Internal)
	}
	return e.Message
}

// Unwrap implements the errors.Unwrap interface
func (e *AppError) Unwrap() error {
	return e.Internal
}

// WithDetails adds details to the error
func (e *AppError) WithDetails(details map[string]interface{}) *AppError {
	e.Details = details
	return e
}

// New creates a new AppError
func New(code string, message string, status int, internal error) *AppError {
	return &AppError{
		Code:     code,
		Message:  message,
		Status:   status,
		Internal: internal,
	}
}

// BadRequest creates a new bad request error
func BadRequest(message string, internal error) *AppError {
	return New("bad_request", message, http.StatusBadRequest, internal)
}

// Unauthorized creates a new unauthorized error
func Unauthorized(message string, internal error) *AppError {
	if message == "" {
		message = "Authentication required"
	}
	return New("unauthorized", message, http.StatusUnauthorized, internal)
}

// Forbidden creates a new forbidden error
func Forbidden(message string, internal error) *AppError {
	if message == "" {
		message = "Access denied"
	}
	return New("forbidden", message, http.StatusForbidden, internal)
}

// NotFound creates a new not found error
func NotFound(message string, internal error) *AppError {
	if message == "" {
		message = "Resource not found"
	}
	return New("not_found", message, http.StatusNotFound, internal)
}

// Internal creates a new internal server error
func Internal(message string, internal error) *AppError {
	if message == "" {
		message = "Internal server error"
	}
	return New("internal_error", message, http.StatusInternalServerError, internal)
}

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr)
}

// AsAppError converts an error to an AppError if possible
func AsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
