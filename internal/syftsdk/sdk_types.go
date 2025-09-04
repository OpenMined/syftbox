package syftsdk

import (
	"fmt"
	"runtime"
	"time"

	"github.com/imroc/req/v3"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/openmined/syftbox/internal/version"
)

const (
	HeaderUserAgent    = "User-Agent"
	HeaderSyftVersion  = "X-Syft-Version"
	HeaderSyftUser     = "X-Syft-User"
	HeaderSyftDeviceId = "X-Syft-Device-Id"
)

var SyftBoxUserAgent = getUserAgent()

func getUserAgent() string {
	osVersion := getOSVersion()
	return fmt.Sprintf("SyftBox/%s (%s; %s/%s; Go/%s; %s)", 
		version.Version, 
		version.Revision, 
		runtime.GOOS, 
		runtime.GOARCH,
		runtime.Version(),
		osVersion,
	)
}

func getOSVersion() string {
	switch runtime.GOOS {
	case "darwin":
		return getMacOSVersion()
	case "linux":
		return getLinuxVersion()
	case "windows":
		return getWindowsVersion()
	default:
		return runtime.GOOS
	}
}

// A simple HTTP client with some common values set
var HTTPClient = req.C().
	SetCommonRetryCount(3).
	SetCommonRetryFixedInterval(1*time.Second).
	SetUserAgent(SyftBoxUserAgent).
	SetCommonHeader(HeaderSyftVersion, version.Version).
	SetCommonHeader(HeaderSyftDeviceId, utils.HWID).
	SetJsonMarshal(jsonMarshal).
	SetJsonUnmarshal(jsonUmarshal)
