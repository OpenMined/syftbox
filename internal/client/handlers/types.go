package handlers

import "github.com/gin-gonic/gin"

const (
	CodeOk                  string = "OK"
	ErrCodeBadRequest       string = "ERR_BAD_REQUEST"
	ErrCodeUnknownError     string = "ERR_UNKNOWN_ERROR"
	ErrCodeDatasiteNotReady string = "ERR_DATASITE_NOT_READY"
)

type ControlPlaneResponse struct {
	Code string `json:"code"`
}

type ControlPlaneError struct {
	ErrorCode string `json:"code"`
	Error     string `json:"error"`
}

func AbortWithError(c *gin.Context, status int, code string, err error) {
	c.Abort()
	c.Error(err)
	c.PureJSON(status, ControlPlaneError{
		ErrorCode: code,
		Error:     err.Error(),
	})
}
