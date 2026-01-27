//go:build darwin || linux

package sync

import (
	"crypto/sha1"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

const hotlinkIPCName = "stream.sock"

func ensureHotlinkIPC(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(hotlinkSocketDir(), 0o755); err != nil {
		return err
	}
	target := hotlinkSocketTarget(path)
	if err := os.WriteFile(path, []byte(target), 0o644); err != nil {
		return err
	}
	return nil
}

func listenHotlinkIPC(path string) (net.Listener, error) {
	if err := ensureHotlinkIPC(path); err != nil {
		return nil, err
	}
	target := hotlinkSocketTarget(path)
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", target)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(target, 0o600); err != nil && err != syscall.EPERM {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func hotlinkSocketDir() string {
	return filepath.Join(string(os.PathSeparator), "tmp", "syftbox-hotlink")
}

func hotlinkSocketTarget(path string) string {
	sum := sha1.Sum([]byte(path))
	name := hex.EncodeToString(sum[:]) + ".sock"
	return filepath.Join(hotlinkSocketDir(), name)
}
