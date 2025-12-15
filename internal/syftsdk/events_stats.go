package syftsdk

import (
	"sync/atomic"
	"time"
)

// wsStats tracks websocket telemetry across reconnects.
type wsStats struct {
	bytesSent      atomic.Int64
	bytesRecv      atomic.Int64
	lastSentNs     atomic.Int64
	lastRecvNs     atomic.Int64
	lastPingNs     atomic.Int64
	connectedAtNs  atomic.Int64
	disconnAtNs    atomic.Int64
	reconnects     atomic.Int64
	lastErrorValue atomic.Value // string
}

func newWSStats() *wsStats {
	s := &wsStats{}
	s.lastErrorValue.Store("")
	return s
}

func (s *wsStats) onConnected() {
	now := time.Now().UnixNano()
	s.connectedAtNs.Store(now)
}

func (s *wsStats) onDisconnected() {
	now := time.Now().UnixNano()
	s.disconnAtNs.Store(now)
	s.reconnects.Add(1)
}

func (s *wsStats) onSend(n int) {
	if n <= 0 {
		return
	}
	s.bytesSent.Add(int64(n))
	s.lastSentNs.Store(time.Now().UnixNano())
}

func (s *wsStats) onRecv(n int) {
	if n <= 0 {
		return
	}
	s.bytesRecv.Add(int64(n))
	s.lastRecvNs.Store(time.Now().UnixNano())
}

func (s *wsStats) onPing() {
	s.lastPingNs.Store(time.Now().UnixNano())
}

func (s *wsStats) setLastError(err error) {
	if err == nil {
		return
	}
	s.lastErrorValue.Store(err.Error())
}

// EventsStatsSnapshot is a stable, JSON-friendly view of websocket state.
type EventsStatsSnapshot struct {
	Connected         bool   `json:"connected"`
	Encoding          string `json:"encoding,omitempty"`
	ReconnectAttempt  int    `json:"reconnect_attempt"`
	Reconnects        int64  `json:"reconnects"`
	TxQueueLen        int    `json:"tx_queue_len"`
	RxQueueLen        int    `json:"rx_queue_len"`
	OverflowQueueLen  int    `json:"overflow_queue_len"`
	BytesSentTotal    int64  `json:"bytes_sent_total"`
	BytesRecvTotal    int64  `json:"bytes_recv_total"`
	ConnectedAtNs     int64  `json:"connected_at_ns,omitempty"`
	DisconnectedAtNs  int64  `json:"disconnected_at_ns,omitempty"`
	LastSentAtNs      int64  `json:"last_sent_at_ns,omitempty"`
	LastRecvAtNs      int64  `json:"last_recv_at_ns,omitempty"`
	LastPingAtNs      int64  `json:"last_ping_at_ns,omitempty"`
	LastError         string `json:"last_error,omitempty"`
}

