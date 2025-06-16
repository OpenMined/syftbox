package api

import "fmt"

type SyftAPIError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *SyftAPIError) Error() string {
	return fmt.Sprintf("syft api error: code=%s, message=%s", e.Code, e.Message)
}
