package sync

import (
	"os"
	"runtime"
	"strings"
)

const (
	hotlinkIPCModeEnv    = "SYFTBOX_HOTLINK_IPC"
	hotlinkIPCTCPAddrEnv = "SYFTBOX_HOTLINK_TCP_ADDR"
	hotlinkIPCModeTCP    = "tcp"
)

func hotlinkIPCMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(hotlinkIPCModeEnv)))
	return mode
}

func hotlinkIPCMarkerName() string {
	if hotlinkIPCMode() == hotlinkIPCModeTCP {
		return "stream.tcp"
	}
	if runtime.GOOS == "windows" {
		return "stream.pipe"
	}
	return "stream.sock"
}

func hotlinkIPCTCPAddr() string {
	return strings.TrimSpace(os.Getenv(hotlinkIPCTCPAddrEnv))
}
