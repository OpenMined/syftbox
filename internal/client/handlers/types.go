package handlers

const (
	CodeOk                  = "OK"
	ErrCodeBadRequest       = "ERR_BAD_REQUEST"
	ErrCodeUnknownError     = "ERR_UNKNOWN_ERROR"
	ErrCodeDatasiteNotReady = "ERR_DATASITE_NOT_READY"
)

type ControlPlaneResponse struct {
	Code string `json:"code"`
}

type ControlPlaneError struct {
	ErrorCode string `json:"code"`
	Error     string `json:"error"`
}
