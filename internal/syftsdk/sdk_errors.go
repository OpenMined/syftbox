package syftsdk

import (
	"errors"
	"fmt"
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

// maps to syft api error codes
const (
	// Generic request/server errors
	CodeInvalidRequest = "E_INVALID_REQUEST" // bad or invalid request
	CodeRateLimited    = "E_RATE_LIMITED"    // rate limit exceeded
	CodeInternalError  = "E_INTERNAL_ERROR"  // internal server error
	CodeAccessDenied   = "E_ACCESS_DENIED"   // access denied

	// Auth errors
	CodeAuthInvalidCredentials    = "E_AUTH_INVALID_CREDENTIALS"     // authentication credentials (e.g., token) are invalid, expired, or malformed.
	CodeAuthTokenGenerationFailed = "E_AUTH_TOKEN_GENERATION_FAILED" // a failure during the generation of new authentication tokens.
	CodeAuthOTPVerificationFailed = "E_AUTH_OTP_VERIFICATION_FAILED" // Email One-Time Password (OTP) verification failed.
	CodeAuthTokenRefreshFailed    = "E_AUTH_TOKEN_REFRESH_FAILED"    // a failure during the attempt to refresh an authentication token.
	CodeAuthNotificationFailed    = "E_AUTH_NOTIFICATION_FAILED"     // a failure in sending an authentication-related notification (e.g., OTP email/SMS).

	// Datasite errors
	CodeDatasiteNotFound        = "E_DATASITE_NOT_FOUND"        // the specified datasite resource could not be found.
	CodeDatasiteInvalidPath     = "E_DATASITE_INVALID_PATH"     // the provided path for a datasite resource is invalid or malformed.
	CodeDatasiteOperationFailed = "E_DATASITE_OPERATION_FAILED" // a generic failure during a datasite operation not covered by other codes.

	// Blob errors
	CodeBlobNotFound             = "E_BLOB_NOT_FOUND"               // the specified blob could not be found.
	CodeBlobInvalidKey           = "E_BLOB_INVALID_KEY"             // the provided key for a blob is invalid (e.g., format, characters, length).
	CodeBlobListFailed           = "E_BLOB_LIST_OPERATION_FAILED"   // a failure during the operation to list blobs.
	CodeBlobPutFailed            = "E_BLOB_PUT_OPERATION_FAILED"    // a failure during the operation to upload/put a blob.
	CodeBlobGetFailed            = "E_BLOB_GET_OPERATION_FAILED"    // a failure during the operation to download/get a blob.
	CodeBlobDeleteFailed         = "E_BLOB_DELETE_OPERATION_FAILED" // a failure during the operation to delete a blob.
	CodeBlobStorageQuotaExceeded = "E_BLOB_STORAGE_QUOTA_EXCEEDED"  // that a storage quota related to blobs has been exceeded.
	CodeBlobOperationConflict    = "E_BLOB_OPERATION_CONFLICT"      // that the blob operation could not be completed due to a conflict with the current state of the resource (e.g. version mismatch).

	// ACL errors
	CodeACLUpdateFailed = "E_ACL_UPDATE_FAILED" // a failure during the operation to update an ACL.
)

// maps to syft api error
type SyftSDKError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *SyftSDKError) Error() string {
	return fmt.Sprintf("sdk: code=%s, message=%s", e.Code, e.Message)
}
