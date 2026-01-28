package sync

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"
)

type hotlinkLocalReader struct {
	markerPath string
	manager    *HotlinkManager
}

func (r *hotlinkLocalReader) run() {
	for {
		conn, err := r.acceptConn()
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		r.readLoop(conn)
	}
}

func (r *hotlinkLocalReader) acceptConn() (net.Conn, error) {
	ipc := r.manager.getIPCWriter(r.markerPath)
	if ipc == nil {
		return nil, fmt.Errorf("hotlink ipc unavailable")
	}
	if err := ipc.EnsureListener(); err != nil {
		return nil, err
	}
	return ipc.AcceptForRead()
}

func (r *hotlinkLocalReader) readLoop(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		frame, err := decodeHotlinkFrame(reader)
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "closed") {
				return
			}
			return
		}
		if frame == nil || len(frame.payload) == 0 || strings.TrimSpace(frame.path) == "" {
			continue
		}
		if latencyTraceEnabled() {
			if ts, ok := payloadTimestampNs(frame.payload); ok {
				slog.Info("latency_trace hotlink_ipc_read", "path", strings.TrimSpace(frame.path), "age_ms", (time.Now().UnixNano()-ts)/1_000_000, "size", len(frame.payload))
			}
		}
		etag := strings.TrimSpace(frame.etag)
		r.manager.sendHotlink(frame.path, etag, frame.payload)
	}
}
