package syftsdk

import (
	"io"
	"sync/atomic"
	"time"
)

// httpStats tracks HTTP traffic for sync/uploads/downloads.
type httpStats struct {
	bytesSent  atomic.Int64
	bytesRecv  atomic.Int64
	lastSentNs atomic.Int64
	lastRecvNs atomic.Int64

	lastErrorValue atomic.Value // string
}

func newHTTPStats() *httpStats {
	s := &httpStats{}
	s.lastErrorValue.Store("")
	return s
}

func (s *httpStats) onSend(n int) {
	if n <= 0 {
		return
	}
	s.bytesSent.Add(int64(n))
	s.lastSentNs.Store(time.Now().UnixNano())
}

func (s *httpStats) onRecv(n int) {
	if n <= 0 {
		return
	}
	s.bytesRecv.Add(int64(n))
	s.lastRecvNs.Store(time.Now().UnixNano())
}

func (s *httpStats) setLastError(err error) {
	if err == nil {
		return
	}
	s.lastErrorValue.Store(err.Error())
}

type countingReadCloser struct {
	rc     io.ReadCloser
	onRead func(int)
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(n)
	}
	return n, err
}

func (c *countingReadCloser) Close() error {
	return c.rc.Close()
}

func wrapCounting(rc io.ReadCloser, onRead func(int)) io.ReadCloser {
	if rc == nil {
		return nil
	}
	return &countingReadCloser{rc: rc, onRead: onRead}
}

type countingReader struct {
	r      io.Reader
	onRead func(int)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(n)
	}
	return n, err
}

// globalHTTPStats is a process-wide sink used by presigned transfers
// that bypass the req.Client. SyftSDK.New sets it once per process.
var globalHTTPStats atomic.Pointer[httpStats]

func setGlobalHTTPStats(s *httpStats) {
	if s == nil {
		return
	}
	globalHTTPStats.Store(s)
}

func getHTTPStats() *httpStats {
	return globalHTTPStats.Load()
}

// HTTPStatsSnapshot is a stable, JSON-friendly view of HTTP traffic.
type HTTPStatsSnapshot struct {
	BytesSentTotal int64  `json:"bytes_sent_total"`
	BytesRecvTotal int64  `json:"bytes_recv_total"`
	LastSentAtNs   int64  `json:"last_sent_at_ns,omitempty"`
	LastRecvAtNs   int64  `json:"last_recv_at_ns,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}
