package syftsdk

const (
	HeaderUserAgent    = "User-Agent"
	HeaderSyftVersion  = "X-Syft-Version"
	HeaderSyftUser     = "X-Syft-User"
	HeaderSyftDeviceId = "X-Syft-Device-Id"
)

type SyftSDKError struct {
	Error string `json:"error"`
}
