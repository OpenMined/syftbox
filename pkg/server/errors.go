package server

const (
	ErrNotFound       = "ERR_NOT_FOUND"
	ErrBadRequest     = "ERR_BAD_REQUEST"
	ErrUploadFailed   = "ERR_UPLOAD_FAILED"
	ErrCompleteFailed = "ERR_COMPLETE"
	ErrListFailed     = "ERR_LIST_FAILED"
)

var (
	ErrResponseNotFound         = ErrorResponse{Code: ErrNotFound, Message: "Not Found"}
	ErrResponseMethodNotAllowed = ErrorResponse{Code: ErrBadRequest, Message: "Method Not Allowed"}
)
