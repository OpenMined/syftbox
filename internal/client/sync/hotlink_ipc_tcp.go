package sync

import (
	"net"
	"os"
	"path/filepath"
	"strings"
)

func ensureHotlinkIPCTCP(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	addr := hotlinkIPCTCPAddr()
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	return os.WriteFile(path, []byte(addr), 0o644)
}

func listenHotlinkIPCTCP(path string) (net.Listener, error) {
	if err := ensureHotlinkIPCTCP(path); err != nil {
		return nil, err
	}
	addr := strings.TrimSpace(hotlinkIPCTCPAddr())
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if tcpAddr := listener.Addr().String(); tcpAddr != "" {
		_ = os.WriteFile(path, []byte(tcpAddr), 0o644)
	}
	return listener, nil
}
