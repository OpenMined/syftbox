package syftsdk

import (
	"errors"
	"fmt"

	"github.com/imroc/req/v3"
)

var (
	// sdk common
	ErrNoRefreshToken = errors.New("sdk: refresh token missing")
	ErrNoServerURL    = errors.New("sdk: server url missing")
	ErrInvalidEmail   = errors.New("sdk: invalid email")

	// auth
	ErrInvalidOTP = errors.New("sdk: invalid otp")

	// blob
	ErrNoPermissions = errors.New("sdk: no permissions")
	ErrFileNotFound  = errors.New("sdk: file not found")
)

const (
	// Generic request/server errors
	CodeInvalidRequest = "E_INVALID_REQUEST" // bad or invalid request
	CodeRateLimited    = "E_RATE_LIMITED"    // rate limit exceeded
	CodeInternalError  = "E_INTERNAL_ERROR"  // internal server error
	CodeAccessDenied   = "E_ACCESS_DENIED"   // access denied
	CodeUnknownError   = "E_UNKNOWN_ERR"     // unknown error

	// Auth errors
	CodeAuthInvalidCredentials    = "E_AUTH_INVALID_CREDENTIALS"     // authentication credentials (e.g., token) are invalid, expired, or malformed.
	CodeAuthTokenGenerationFailed = "E_AUTH_TOKEN_GENERATION_FAILED" // a failure during the generation of new authentication tokens.
	CodeAuthOTPVerificationFailed = "E_AUTH_OTP_VERIFICATION_FAILED" // Email One-Time Password (OTP) verification failed.
	CodeAuthTokenRefreshFailed    = "E_AUTH_TOKEN_REFRESH_FAILED"    // a failure during the attempt to refresh an authentication token.
	CodeAuthNotificationFailed    = "E_AUTH_NOTIFICATION_FAILED"     // a failure in sending an authentication-related notification (e.g., OTP email/SMS).

	// Datasite errors
	CodeDatasiteNotFound    = "E_DATASITE_NOT_FOUND"    // the specified datasite resource could not be found.
	CodeDatasiteInvalidPath = "E_DATASITE_INVALID_PATH" // the provided path for a datasite resource is invalid or malformed.

	// Blob errors
	CodeBlobNotFound     = "E_BLOB_NOT_FOUND"               // the specified blob could not be found.
	CodeBlobListFailed   = "E_BLOB_LIST_OPERATION_FAILED"   // a failure during the operation to list blobs.
	CodeBlobPutFailed    = "E_BLOB_PUT_OPERATION_FAILED"    // a failure during the operation to upload/put a blob.
	CodeBlobGetFailed    = "E_BLOB_GET_OPERATION_FAILED"    // a failure during the operation to download/get a blob.
	CodeBlobDeleteFailed = "E_BLOB_DELETE_OPERATION_FAILED" // a failure during the operation to delete a blob.

	// ACL errors
	CodeACLUpdateFailed = "E_ACL_UPDATE_FAILED" // a failure during the operation to update an ACL.
)

type SDKError interface {
	error
	ErrorCode() string
	ErrorMessage() string
}

// BaseError provides common error functionality
type BaseError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *BaseError) ErrorCode() string    { return e.Code }
func (e *BaseError) ErrorMessage() string { return e.Message }

// APIError represents SyftBox API errors
type APIError struct {
	BaseError
}

func NewAPIError(code, message string) *APIError {
	return &APIError{
		BaseError: BaseError{
			Code:    code,
			Message: message,
		},
	}
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error: %s - %s", e.Code, e.Message)
}

var _ SDKError = (*APIError)(nil)

// PresignedURLError represents presigned URL specific errors
type PresignedURLError struct {
	BaseError
}

func NewPresignedURLError(code, message string) *PresignedURLError {
	return &PresignedURLError{
		BaseError: BaseError{
			Code:    code,
			Message: message,
		},
	}
}

func (e *PresignedURLError) Error() string {
	return fmt.Sprintf("presigned url error: %s - %s", e.Code, e.Message)
}

var _ SDKError = (*PresignedURLError)(nil)

// handleAPIError is a helper function that handles the common error pattern
func handleAPIError(resp *req.Response, requestErr error, operation string) error {
	if requestErr != nil {
		return fmt.Errorf("http request error: %s %w", operation, requestErr)
	}

	// got a response, but api returned an error
	if resp.IsErrorState() {
		if err, ok := resp.ErrorResult().(*APIError); ok {
			return fmt.Errorf("%s %w", operation, err)
		}

		return fmt.Errorf("api error: %s %s", operation, resp.Dump())
	}

	return nil
}
