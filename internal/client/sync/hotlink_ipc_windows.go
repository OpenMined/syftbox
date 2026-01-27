//go:build windows

package sync

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/Microsoft/go-winio"
)

const hotlinkIPCName = "stream.pipe"

func ensureHotlinkIPC(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	pipeName := hotlinkPipeName(path)
	// Marker file for discovery by apps.
	return os.WriteFile(path, []byte(pipeName), 0o644)
}

func listenHotlinkIPC(path string) (net.Listener, error) {
	if err := ensureHotlinkIPC(path); err != nil {
		return nil, err
	}
	pipeName := hotlinkPipeName(path)
	cfg := &winio.PipeConfig{
		InputBufferSize:  64 * 1024,
		OutputBufferSize: 64 * 1024,
		MessageMode:      false,
	}
	return winio.ListenPipe(pipeName, cfg)
}

func hotlinkPipeName(path string) string {
	sum := sha1.Sum([]byte(path))
	return fmt.Sprintf("\\\\.\\pipe\\syftbox-hotlink-%s", hex.EncodeToString(sum[:]))
}
