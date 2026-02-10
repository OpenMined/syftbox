package sync

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	quic "github.com/quic-go/quic-go"

	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	hotlinkEnabledEnv        = "SYFTBOX_HOTLINK"
	hotlinkSocketOnlyEnv     = "SYFTBOX_HOTLINK_SOCKET_ONLY"
	hotlinkTCPProxyEnv       = "SYFTBOX_HOTLINK_TCP_PROXY"
	hotlinkTCPProxyAddr      = "SYFTBOX_HOTLINK_TCP_PROXY_ADDR"
	hotlinkStunServerEnv     = "SYFTBOX_HOTLINK_STUN_SERVER"
	hotlinkQuicEnv           = "SYFTBOX_HOTLINK_QUIC"
	hotlinkQuicOnlyEnv       = "SYFTBOX_HOTLINK_QUIC_ONLY"
	hotlinkAcceptName        = "stream.accept"
	hotlinkAcceptDelay       = 200 * time.Millisecond
	hotlinkAcceptTimeout     = 1500 * time.Millisecond
	hotlinkQuicDialTimeout   = 1500 * time.Millisecond
	hotlinkQuicAcceptTimeout = 2500 * time.Millisecond
	hotlinkFrameMagic        = "HLNK"
	hotlinkFrameVersion      = 1
	hotlinkDedupeMax         = 1024
	hotlinkConnectTimeout    = 5 * time.Second
	hotlinkStunTimeout       = 500 * time.Millisecond
	hotlinkTCPMarkerName     = "stream.tcp"
	hotlinkTCPSuffix         = "stream.tcp.request"
	hotlinkQuicALPN          = "syftbox-hotlink"
)

type hotlinkSession struct {
	id         string
	path       string
	dirAbs     string
	ipcPath    string
	acceptPath string
	done       chan struct{}
	quic       *hotlinkQuicSession
}

type hotlinkOutbound struct {
	id               string
	pathKey          string
	accept           chan struct{}
	reject           chan string
	seq              uint64
	accepted         bool
	wsFallbackLogged bool
	mu               sync.Mutex
	quic             *hotlinkQuicOutbound
}

type hotlinkQuicSession struct {
	listener  *quic.Listener
	conn      *quic.Conn
	stream    *quic.Stream
	addr      string
	ready     chan struct{}
	readyOnce sync.Once
	err       error
	mu        sync.Mutex
}

type hotlinkQuicOutbound struct {
	conn      *quic.Conn
	stream    *quic.Stream
	ready     chan struct{}
	readyOnce sync.Once
	err       error
	mu        sync.Mutex
}

type HotlinkManager struct {
	workspace   *workspace.Workspace
	sdk         *syftsdk.SyftSDK
	enabled     bool
	socketOnly  bool
	tcpProxy    bool
	quicEnabled bool
	quicOnly    bool

	mu       sync.RWMutex
	sessions map[string]*hotlinkSession

	outMu          sync.RWMutex
	outbound       map[string]*hotlinkOutbound
	outboundByPath map[string]*hotlinkOutbound

	dedupe *hotlinkDedupe

	ipcMu      sync.Mutex
	ipcWriters map[string]*hotlinkIPC

	localMu      sync.Mutex
	localReaders map[string]*hotlinkLocalReader

	tcpMu      sync.Mutex
	tcpProxies map[string]struct{}
	tcpWriters map[string]net.Conn
	tcpReorder map[string]*tcpReorderBuf
}

type tcpReorderBuf struct {
	nextSeq uint64
	pending map[uint64][]byte
}

func NewHotlinkManager(ws *workspace.Workspace, sdk *syftsdk.SyftSDK) *HotlinkManager {
	manager := &HotlinkManager{
		workspace:      ws,
		sdk:            sdk,
		enabled:        os.Getenv(hotlinkEnabledEnv) == "1",
		socketOnly:     os.Getenv(hotlinkSocketOnlyEnv) == "1",
		tcpProxy:       os.Getenv(hotlinkTCPProxyEnv) == "1",
		quicEnabled:    strings.TrimSpace(os.Getenv(hotlinkQuicEnv)) != "0",
		quicOnly:       os.Getenv(hotlinkQuicOnlyEnv) == "1",
		sessions:       make(map[string]*hotlinkSession),
		outbound:       make(map[string]*hotlinkOutbound),
		outboundByPath: make(map[string]*hotlinkOutbound),
		dedupe:         newHotlinkDedupe(hotlinkDedupeMax),
		ipcWriters:     make(map[string]*hotlinkIPC),
		localReaders:   make(map[string]*hotlinkLocalReader),
		tcpProxies:     make(map[string]struct{}),
		tcpWriters:     make(map[string]net.Conn),
		tcpReorder:     make(map[string]*tcpReorderBuf),
	}
	if manager.enabled {
		slog.Info("hotlink config",
			"socket_only", manager.socketOnly,
			"tcp_proxy", manager.tcpProxy,
			"quic_enabled", manager.quicEnabled,
			"quic_only", manager.quicOnly,
		)
	}
	return manager
}

func (h *HotlinkManager) Enabled() bool {
	return h.enabled
}

func (h *HotlinkManager) SocketOnly() bool {
	return h.socketOnly
}

func (h *HotlinkManager) TCPProxyEnabled() bool {
	return h.tcpProxy
}

func (h *HotlinkManager) StartLocalReaders(ctx context.Context) {
	if !h.enabled || !h.socketOnly {
		return
	}
	go h.scanLocalReaders(ctx)
}

func (h *HotlinkManager) StartTCPProxyDiscovery(ctx context.Context) {
	if !h.enabled || !h.tcpProxy {
		return
	}
	go h.scanTCPProxies(ctx)
}

func (h *HotlinkManager) scanLocalReaders(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.discoverLocalReaders()
		}
	}
}

func (h *HotlinkManager) discoverLocalReaders() {
	patterns := []string{
		filepath.Join(h.workspace.UserDir, "app_data", "*", "rpc", "*", hotlinkIPCMarkerName()),
		filepath.Join(h.workspace.UserDir, "shared", "flows", "*", "*", "_mpc", "*", hotlinkIPCMarkerName()),
	}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil || len(paths) == 0 {
			continue
		}
		for _, markerPath := range paths {
			h.localMu.Lock()
			if _, exists := h.localReaders[markerPath]; exists {
				h.localMu.Unlock()
				continue
			}
			reader := &hotlinkLocalReader{
				markerPath: markerPath,
				manager:    h,
			}
			h.localReaders[markerPath] = reader
			h.localMu.Unlock()
			go reader.run()
		}
	}
}

func (h *HotlinkManager) scanTCPProxies(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.discoverTCPProxies()
		}
	}
}

func (h *HotlinkManager) discoverTCPProxies() {
	_ = filepath.WalkDir(h.workspace.DatasitesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != hotlinkTCPMarkerName {
			return nil
		}
		rel, relErr := h.workspace.DatasiteRelPath(path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(rel, "/_mpc/") {
			return nil
		}
		info, infoErr := readTCPMarkerInfo(path, rel, h.workspace.Owner)
		if infoErr != nil {
			return nil
		}
		channelKey := canonicalTCPKey(rel, info)
		if channelKey == "" {
			return nil
		}
		h.tcpMu.Lock()
		if _, exists := h.tcpProxies[channelKey]; exists {
			h.tcpMu.Unlock()
			return nil
		}
		h.tcpProxies[channelKey] = struct{}{}
		h.tcpMu.Unlock()
		go h.runTCPProxy(rel, info, channelKey)
		return nil
	})
}

func (h *HotlinkManager) HandleOpen(msg *syftmsg.Message) {
	if !h.enabled {
		return
	}

	open, ok := hotlinkOpenFromMsg(msg)
	if !ok {
		slog.Error("hotlink open invalid payload", "msgId", msg.Id)
		return
	}

	dirRel := open.Path
	if isHotlinkEligible(open.Path) {
		dirRel = filepath.Dir(open.Path)
	}
	dirAbs := h.workspace.DatasiteAbsPath(dirRel)
	if err := utils.EnsureDir(dirAbs); err != nil {
		slog.Error("hotlink open ensure dir", "path", dirAbs, "error", err)
		return
	}

	session := &hotlinkSession{
		id:         open.SessionID,
		path:       open.Path,
		dirAbs:     dirAbs,
		ipcPath:    filepath.Join(dirAbs, hotlinkIPCMarkerName()),
		acceptPath: filepath.Join(dirAbs, hotlinkAcceptName),
		done:       make(chan struct{}),
	}

	if err := ensureHotlinkIPC(session.ipcPath); err != nil {
		slog.Error("hotlink open ipc setup failed", "path", session.ipcPath, "error", err)
		_ = h.sdk.Events.Send(syftmsg.NewHotlinkReject(open.SessionID, "ipc unavailable"))
		return
	}

	if writer := h.getIPCWriter(session.ipcPath); writer != nil {
		if err := writer.EnsureListener(); err != nil {
			slog.Warn("hotlink ipc listen failed", "path", session.ipcPath, "error", err)
		}
	}

	h.mu.Lock()
	h.sessions[session.id] = session
	h.mu.Unlock()

	if isTCPProxyPath(open.Path) {
		if err := h.sdk.Events.Send(syftmsg.NewHotlinkAccept(session.id)); err != nil {
			slog.Warn("hotlink accept send failed", "session", session.id, "error", err)
		}
		h.maybeStartQuicOffer(session)
		return
	}

	if utils.FileExists(session.acceptPath) {
		if err := h.sdk.Events.Send(syftmsg.NewHotlinkAccept(session.id)); err != nil {
			slog.Warn("hotlink accept send failed", "session", session.id, "error", err)
		}
		h.maybeStartQuicOffer(session)
		return
	}
	go h.waitForAccept(session)
}

func (h *HotlinkManager) HandleAccept(msg *syftmsg.Message) {
	if !h.enabled {
		return
	}
	accept, ok := hotlinkAcceptFromMsg(msg)
	if !ok {
		slog.Error("hotlink accept invalid payload", "msgId", msg.Id)
		return
	}

	h.outMu.RLock()
	out := h.outbound[accept.SessionID]
	h.outMu.RUnlock()
	if out != nil {
		out.mu.Lock()
		if !out.accepted {
			out.accepted = true
			close(out.accept)
		}
		out.mu.Unlock()
	}
}

func (h *HotlinkManager) HandleReject(msg *syftmsg.Message) {
	if !h.enabled {
		return
	}
	reject, ok := hotlinkRejectFromMsg(msg)
	if !ok {
		slog.Error("hotlink reject invalid payload", "msgId", msg.Id)
		return
	}
	if out := h.removeOutbound(reject.SessionID); out != nil {
		select {
		case out.reject <- reject.Reason:
		default:
		}
		return
	}
	h.closeSession(reject.SessionID)
}

func (h *HotlinkManager) HandleData(msg *syftmsg.Message) {
	if !h.enabled {
		return
	}
	if h.quicOnly {
		slog.Info("hotlink ws data ignored (quic-only)")
		return
	}
	data, ok := hotlinkDataFromMsg(msg)
	if !ok {
		slog.Error("hotlink data invalid payload", "msgId", msg.Id)
		return
	}

	h.mu.RLock()
	session := h.sessions[data.SessionID]
	h.mu.RUnlock()
	if session == nil {
		return
	}

	if len(data.Payload) == 0 {
		return
	}

	etag := strings.TrimSpace(data.ETag)
	if etag == "" {
		etag = fmt.Sprintf("%x", md5.Sum(data.Payload))
	}

	framePath := session.path
	if strings.TrimSpace(data.Path) != "" {
		framePath = data.Path
	}
	h.handleHotlinkPayload(session, framePath, etag, data.Seq, data.Payload)
}

func (h *HotlinkManager) handleHotlinkPayload(session *hotlinkSession, framePath string, etag string, seq uint64, payload []byte) {
	if session == nil || len(payload) == 0 {
		return
	}
	writer := h.getIPCWriter(session.ipcPath)
	if writer == nil {
		return
	}
	if h.dedupe.Seen(session.path, etag) {
		return
	}
	if isTCPProxyPath(framePath) {
		tcpWriter := h.getTCPWriterWithRetry(framePath)
		if tcpWriter == nil {
			slog.Error("hotlink tcp write skipped: no writer after retries", "path", framePath)
			return
		}
		h.tcpMu.Lock()
		buf := h.tcpReorder[framePath]
		if buf == nil {
			buf = &tcpReorderBuf{nextSeq: 1, pending: make(map[uint64][]byte)}
			h.tcpReorder[framePath] = buf
		}
		buf.pending[seq] = payload
		var toWrite [][]byte
		for {
			data, ok := buf.pending[buf.nextSeq]
			if !ok {
				break
			}
			delete(buf.pending, buf.nextSeq)
			buf.nextSeq++
			toWrite = append(toWrite, data)
		}
		h.tcpMu.Unlock()
		for _, data := range toWrite {
			if _, err := tcpWriter.Write(data); err != nil {
				slog.Warn("hotlink tcp write failed", "path", framePath, "error", err)
				break
			}
		}
		return
	}
	frame := encodeHotlinkFrame(framePath, etag, seq, payload)
	if err := writer.Write(frame); err != nil {
		slog.Warn("hotlink ipc write failed", "session", session.id, "error", err)
	} else {
		slog.Debug("hotlink ipc wrote", "session", session.id, "bytes", len(frame))
		if latencyTraceEnabled() {
			if ts, ok := payloadTimestampNs(payload); ok {
				slog.Info("latency_trace hotlink_ipc_written", "path", framePath, "age_ms", (time.Now().UnixNano()-ts)/1_000_000, "size", len(payload))
			}
		}
	}
}

func (h *HotlinkManager) HandleClose(msg *syftmsg.Message) {
	if !h.enabled {
		return
	}
	closeMsg, ok := hotlinkCloseFromMsg(msg)
	if !ok {
		slog.Error("hotlink close invalid payload", "msgId", msg.Id)
		return
	}

	if out := h.removeOutbound(closeMsg.SessionID); out != nil {
		select {
		case out.reject <- closeMsg.Reason:
		default:
		}
		return
	}

	session := h.closeSession(closeMsg.SessionID)
	if session != nil && closeMsg.Reason == "fallback" {
		go h.replayFallback(session)
	}
}

func (h *HotlinkManager) HandleSignal(msg *syftmsg.Message) {
	if !h.enabled || !h.quicEnabled {
		return
	}
	signal, ok := hotlinkSignalFromMsg(msg)
	if !ok {
		slog.Error("hotlink signal invalid payload", "msgId", msg.Id)
		return
	}

	switch signal.Kind {
	case "quic_offer":
		h.outMu.RLock()
		out := h.outbound[signal.SessionID]
		h.outMu.RUnlock()
		if out == nil {
			return
		}
		go h.handleQuicOffer(out, signal)
	case "quic_answer":
		h.handleQuicAnswer(signal)
	case "quic_error":
		slog.Warn("hotlink quic error", "session", signal.SessionID, "error", signal.Error)
	default:
		slog.Debug("hotlink signal ignored", "session", signal.SessionID, "kind", signal.Kind)
	}
}

func (h *HotlinkManager) waitForAccept(session *hotlinkSession) {
	ticker := time.NewTicker(hotlinkAcceptDelay)
	defer ticker.Stop()

	for {
		select {
		case <-session.done:
			return
		case <-ticker.C:
			if !utils.FileExists(session.acceptPath) {
				continue
			}
			if err := h.sdk.Events.Send(syftmsg.NewHotlinkAccept(session.id)); err != nil {
				slog.Warn("hotlink accept send failed", "session", session.id, "error", err)
			}
			h.maybeStartQuicOffer(session)
			return
		}
	}
}

func (h *HotlinkManager) closeSession(id string) *hotlinkSession {
	h.mu.Lock()
	session := h.sessions[id]
	if session != nil {
		delete(h.sessions, id)
	}
	h.mu.Unlock()

	if session == nil {
		return nil
	}

	close(session.done)
	return session
}

func (h *HotlinkManager) replayFallback(session *hotlinkSession) {
	if session == nil {
		return
	}
	pattern := filepath.Join(session.dirAbs, "*.request")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return
	}
	sort.Strings(files)

	var seq uint64
	for _, path := range files {
		payload, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(payload) == 0 {
			continue
		}
		rel, err := h.workspace.DatasiteRelPath(path)
		if err != nil {
			continue
		}
		etag := fmt.Sprintf("%x", md5.Sum(payload))
		if h.dedupe.Seen(rel, etag) {
			continue
		}
		seq++
		frame := encodeHotlinkFrame(rel, etag, seq, payload)
		writer := h.getIPCWriter(session.ipcPath)
		if writer == nil {
			return
		}
		if err := writer.Write(frame); err != nil {
			return
		}
	}
}

func (h *HotlinkManager) maybeStartQuicOffer(session *hotlinkSession) {
	if session == nil || !h.quicEnabled {
		return
	}
	if session.quic != nil {
		return
	}

	tlsConf, err := newQuicServerTLSConfig()
	if err != nil {
		slog.Warn("hotlink quic tls setup failed", "session", session.id, "error", err)
		return
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		slog.Warn("hotlink quic udp bind failed", "session", session.id, "error", err)
		return
	}

	transport := &quic.Transport{Conn: udpConn}
	listener, err := transport.Listen(tlsConf, nil)
	if err != nil {
		slog.Warn("hotlink quic listen failed", "session", session.id, "error", err)
		_ = udpConn.Close()
		return
	}

	addr := udpConn.LocalAddr().String()
	stunAddr, stunErr := discoverStunAddr(udpConn)
	if stunErr != nil {
		slog.Debug("hotlink quic stun discovery failed", "session", session.id, "error", stunErr)
	}

	q := &hotlinkQuicSession{
		listener: listener,
		addr:     addr,
		ready:    make(chan struct{}),
	}
	session.quic = q

	offerAddrs := quicOfferAddrs(addr, stunAddr)
	if err := h.sdk.Events.Send(syftmsg.NewHotlinkSignal(session.id, "quic_offer", offerAddrs, "", "")); err != nil {
		slog.Warn("hotlink quic offer send failed", "session", session.id, "error", err)
	}

	go h.acceptQuic(session)
}

func (h *HotlinkManager) acceptQuic(session *hotlinkSession) {
	if session == nil || session.quic == nil {
		return
	}
	q := session.quic
	ctx, cancel := context.WithTimeout(context.Background(), hotlinkQuicAcceptTimeout)
	defer cancel()

	conn, err := q.listener.Accept(ctx)
	if err != nil {
		q.mu.Lock()
		q.err = err
		q.mu.Unlock()
		q.readyOnce.Do(func() { close(q.ready) })
		slog.Info("hotlink quic accept timeout, ws fallback", "session", session.id, "error", err)
		return
	}

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		q.mu.Lock()
		q.err = err
		q.mu.Unlock()
		q.readyOnce.Do(func() { close(q.ready) })
		slog.Info("hotlink quic accept stream failed, ws fallback", "session", session.id, "error", err)
		return
	}

	reader := bufio.NewReader(stream)
	if err := readQuicHandshake(reader, session.id); err != nil {
		q.mu.Lock()
		q.err = err
		q.mu.Unlock()
		q.readyOnce.Do(func() { close(q.ready) })
		_ = stream.Close()
		slog.Warn("hotlink quic handshake failed", "session", session.id, "error", err)
		return
	}

	q.mu.Lock()
	q.conn = conn
	q.stream = stream
	q.mu.Unlock()
	q.readyOnce.Do(func() { close(q.ready) })
	slog.Info("hotlink quic connected", "session", session.id, "addr", conn.RemoteAddr().String())

	h.runQuicReader(session, reader)
}

func (h *HotlinkManager) handleQuicOffer(out *hotlinkOutbound, signal syftmsg.HotlinkSignal) {
	if out == nil || out.quic == nil {
		return
	}
	if len(signal.Addrs) == 0 {
		out.quic.mu.Lock()
		out.quic.err = fmt.Errorf("quic offer missing addresses")
		out.quic.mu.Unlock()
		out.quic.readyOnce.Do(func() { close(out.quic.ready) })
		_ = h.sdk.Events.Send(syftmsg.NewHotlinkSignal(out.id, "quic_answer", nil, "", "offer missing addresses"))
		return
	}

	tlsConf := newQuicClientTLSConfig()
	var lastErr error
	for _, addr := range signal.Addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), hotlinkQuicDialTimeout)
		conn, err := quic.DialAddr(ctx, addr, tlsConf, nil)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		ctx, cancel = context.WithTimeout(context.Background(), hotlinkQuicDialTimeout)
		stream, err := conn.OpenStreamSync(ctx)
		cancel()
		if err != nil {
			lastErr = err
			_ = conn.CloseWithError(0, "stream error")
			continue
		}
		if err := writeQuicHandshake(stream, out.id); err != nil {
			lastErr = err
			_ = stream.Close()
			_ = conn.CloseWithError(0, "handshake error")
			continue
		}
		out.quic.mu.Lock()
		out.quic.conn = conn
		out.quic.stream = stream
		out.quic.err = nil
		out.quic.mu.Unlock()
		out.quic.readyOnce.Do(func() { close(out.quic.ready) })
		_ = h.sdk.Events.Send(syftmsg.NewHotlinkSignal(out.id, "quic_answer", []string{addr}, "ok", ""))
		slog.Info("hotlink quic dialed", "session", out.id, "addr", addr)
		return
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("quic dial failed")
	}
	out.quic.mu.Lock()
	out.quic.err = lastErr
	out.quic.mu.Unlock()
	out.quic.readyOnce.Do(func() { close(out.quic.ready) })
	_ = h.sdk.Events.Send(syftmsg.NewHotlinkSignal(out.id, "quic_answer", nil, "", lastErr.Error()))
	slog.Info("hotlink quic dial failed, ws fallback", "session", out.id, "error", lastErr)
}

func (h *HotlinkManager) handleQuicAnswer(signal syftmsg.HotlinkSignal) {
	h.mu.RLock()
	session := h.sessions[signal.SessionID]
	h.mu.RUnlock()
	if session == nil || session.quic == nil {
		return
	}
	if signal.Error != "" {
		slog.Info("hotlink quic answer error, ws fallback", "session", signal.SessionID, "error", signal.Error)
		if h.quicOnly {
			_ = h.sdk.Events.Send(syftmsg.NewHotlinkClose(signal.SessionID, "quic-only"))
		}
		return
	}
	slog.Info("hotlink quic answer ok", "session", signal.SessionID, "addr", strings.Join(signal.Addrs, ","))
}

func (h *HotlinkManager) trySendQuic(out *hotlinkOutbound, relPath string, etag string, seq uint64, payload []byte, wait bool) (bool, error) {
	if out == nil || out.quic == nil {
		return false, nil
	}
	if wait {
		select {
		case <-out.quic.ready:
		case <-time.After(hotlinkQuicAcceptTimeout):
			return false, fmt.Errorf("quic wait timeout")
		}
	} else {
		select {
		case <-out.quic.ready:
		default:
			return false, nil
		}
	}

	out.quic.mu.Lock()
	stream := out.quic.stream
	err := out.quic.err
	out.quic.mu.Unlock()
	if err != nil {
		return false, err
	}
	if stream == nil {
		return false, fmt.Errorf("quic stream unavailable")
	}
	frame := encodeHotlinkFrame(relPath, etag, seq, payload)
	if _, err := stream.Write(frame); err != nil {
		out.quic.mu.Lock()
		out.quic.err = err
		out.quic.mu.Unlock()
		return false, err
	}
	return true, nil
}

func (h *HotlinkManager) runQuicReader(session *hotlinkSession, reader *bufio.Reader) {
	for {
		frame, err := decodeHotlinkFrame(reader)
		if err != nil {
			if err != io.EOF {
				slog.Warn("hotlink quic read failed", "session", session.id, "error", err)
			}
			return
		}
		if frame == nil {
			continue
		}
		h.handleHotlinkPayload(session, frame.path, frame.etag, frame.seq, frame.payload)
	}
}

type hotlinkIPC struct {
	path         string
	mu           sync.Mutex
	listener     net.Listener
	conn         net.Conn
	readerActive bool
}

func (f *hotlinkIPC) EnsureListener() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listener != nil {
		return nil
	}
	listener, err := listenHotlinkIPC(f.path)
	if err != nil {
		return err
	}
	f.listener = listener
	return nil
}

func (f *hotlinkIPC) Write(payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listener == nil {
		listener, err := listenHotlinkIPC(f.path)
		if err != nil {
			return err
		}
		f.listener = listener
	}
	if f.conn == nil {
		conn, err := acceptHotlinkConn(f.listener, hotlinkConnectTimeout)
		if err != nil {
			return err
		}
		if f.conn != nil {
			_ = f.conn.Close()
		}
		f.conn = conn
	}
	if _, err := f.conn.Write(payload); err != nil {
		_ = f.conn.Close()
		f.conn = nil
		return err
	}
	return nil
}

func (f *hotlinkIPC) AcceptForRead() (net.Conn, error) {
	f.mu.Lock()
	if f.listener == nil {
		listener, err := listenHotlinkIPC(f.path)
		if err != nil {
			f.mu.Unlock()
			return nil, err
		}
		f.listener = listener
	}
	listener := f.listener
	f.mu.Unlock()

	conn, err := acceptHotlinkConn(listener, hotlinkConnectTimeout)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.conn != nil {
		_ = f.conn.Close()
	}
	f.conn = conn
	f.readerActive = true
	f.mu.Unlock()
	return conn, nil
}

func (h *HotlinkManager) getIPCWriter(path string) *hotlinkIPC {
	h.ipcMu.Lock()
	defer h.ipcMu.Unlock()
	w := h.ipcWriters[path]
	if w == nil {
		w = &hotlinkIPC{path: path}
		h.ipcWriters[path] = w
	}
	return w
}

func hotlinkOpenFromMsg(msg *syftmsg.Message) (syftmsg.HotlinkOpen, bool) {
	switch v := msg.Data.(type) {
	case syftmsg.HotlinkOpen:
		return v, true
	case *syftmsg.HotlinkOpen:
		return *v, true
	default:
		return syftmsg.HotlinkOpen{}, false
	}
}

func hotlinkAcceptFromMsg(msg *syftmsg.Message) (syftmsg.HotlinkAccept, bool) {
	switch v := msg.Data.(type) {
	case syftmsg.HotlinkAccept:
		return v, true
	case *syftmsg.HotlinkAccept:
		return *v, true
	default:
		return syftmsg.HotlinkAccept{}, false
	}
}

func hotlinkRejectFromMsg(msg *syftmsg.Message) (syftmsg.HotlinkReject, bool) {
	switch v := msg.Data.(type) {
	case syftmsg.HotlinkReject:
		return v, true
	case *syftmsg.HotlinkReject:
		return *v, true
	default:
		return syftmsg.HotlinkReject{}, false
	}
}

func hotlinkDataFromMsg(msg *syftmsg.Message) (syftmsg.HotlinkData, bool) {
	switch v := msg.Data.(type) {
	case syftmsg.HotlinkData:
		return v, true
	case *syftmsg.HotlinkData:
		return *v, true
	default:
		return syftmsg.HotlinkData{}, false
	}
}

func hotlinkCloseFromMsg(msg *syftmsg.Message) (syftmsg.HotlinkClose, bool) {
	switch v := msg.Data.(type) {
	case syftmsg.HotlinkClose:
		return v, true
	case *syftmsg.HotlinkClose:
		return *v, true
	default:
		return syftmsg.HotlinkClose{}, false
	}
}

func hotlinkSignalFromMsg(msg *syftmsg.Message) (syftmsg.HotlinkSignal, bool) {
	switch v := msg.Data.(type) {
	case syftmsg.HotlinkSignal:
		return v, true
	case *syftmsg.HotlinkSignal:
		return *v, true
	default:
		return syftmsg.HotlinkSignal{}, false
	}
}

func (h *HotlinkManager) SendBestEffort(relPath string, etag string, payload []byte) {
	if !h.enabled {
		return
	}
	if !isHotlinkEligible(relPath) {
		return
	}
	if len(payload) == 0 {
		return
	}
	if strings.TrimSpace(etag) == "" {
		etag = fmt.Sprintf("%x", md5.Sum(payload))
	}
	go func() {
		if err := h.sendHotlink(relPath, etag, payload); err != nil {
			slog.Warn("hotlink send failed", "path", relPath, "error", err)
		}
	}()
}

func (h *HotlinkManager) sendHotlink(relPath string, etag string, payload []byte) error {
	pathKey := filepath.Dir(relPath)
	out := h.getOrOpenOutbound(pathKey, relPath)
	if out == nil {
		return fmt.Errorf("hotlink outbound unavailable")
	}

	if !h.waitAccepted(out, hotlinkAcceptTimeout) {
		_ = h.sdk.Events.Send(syftmsg.NewHotlinkClose(out.id, "fallback"))
		h.removeOutbound(out.id)
		return fmt.Errorf("hotlink accept timeout")
	}

	out.mu.Lock()
	out.seq++
	seq := out.seq
	out.mu.Unlock()

	if h.quicEnabled && out.quic != nil {
		wait := h.quicOnly
		if ok, err := h.trySendQuic(out, relPath, etag, seq, payload, wait); ok {
			return nil
		} else if err != nil && h.quicOnly {
			return err
		}
		if h.quicOnly {
			return fmt.Errorf("hotlink quic unavailable")
		}
		out.mu.Lock()
		if !out.wsFallbackLogged {
			out.wsFallbackLogged = true
			slog.Info("hotlink quic not ready, using ws fallback", "session", out.id, "path", relPath)
		}
		out.mu.Unlock()
	}

	if err := h.sdk.Events.Send(syftmsg.NewHotlinkData(out.id, seq, relPath, etag, payload)); err != nil {
		_ = h.sdk.Events.Send(syftmsg.NewHotlinkClose(out.id, "fallback"))
		h.removeOutbound(out.id)
		return err
	}
	return nil
}

func (h *HotlinkManager) getOrOpenOutbound(pathKey, relPath string) *hotlinkOutbound {
	h.outMu.RLock()
	existing := h.outboundByPath[pathKey]
	h.outMu.RUnlock()
	if existing != nil {
		return existing
	}
	return h.openOutbound(pathKey, relPath)
}

func (h *HotlinkManager) getTCPWriter(path string) net.Conn {
	h.tcpMu.Lock()
	defer h.tcpMu.Unlock()
	return h.tcpWriters[path]
}

func (h *HotlinkManager) getTCPWriterWithRetry(path string) net.Conn {
	if w := h.getTCPWriter(path); w != nil {
		return w
	}
	slog.Debug("hotlink tcp writer not ready, waiting", "path", path)
	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		if w := h.getTCPWriter(path); w != nil {
			slog.Debug("hotlink tcp writer ready after wait", "path", path)
			return w
		}
	}
	return nil
}

func (h *HotlinkManager) setTCPWriter(path string, conn net.Conn) {
	h.tcpMu.Lock()
	h.tcpWriters[path] = conn
	h.tcpMu.Unlock()
}

func (h *HotlinkManager) clearTCPWriter(path string) {
	h.tcpMu.Lock()
	delete(h.tcpWriters, path)
	h.tcpMu.Unlock()
}

func (h *HotlinkManager) runTCPProxy(relMarker string, info *tcpMarkerInfo, channelKey string) {
	localKey := localTCPKey(relMarker)
	bindIP := tcpProxyBindIP()
	addr := fmt.Sprintf("%s:%d", bindIP, info.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("hotlink tcp proxy bind failed", "addr", addr, "error", err)
		return
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		h.setTCPWriter(channelKey, conn)
		if localKey != "" {
			h.setTCPWriter(localKey, conn)
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 64*1024)
			for {
				n, readErr := c.Read(buf)
				if n == 0 || readErr != nil {
					break
				}
				payload := make([]byte, n)
				copy(payload, buf[:n])
				if err := h.sendHotlink(channelKey, "", payload); err != nil {
					break
				}
			}
			h.clearTCPWriter(channelKey)
			if localKey != "" {
				h.clearTCPWriter(localKey)
			}
		}(conn)
	}
}

type tcpMarkerInfo struct {
	From    string         `json:"from"`
	To      string         `json:"to"`
	Port    int            `json:"port"`
	Ports   map[string]int `json:"ports"`
	FromPID int
	ToPID   int
}

func readTCPMarkerInfo(markerAbs string, relMarker string, localEmail string) (*tcpMarkerInfo, error) {
	content, err := os.ReadFile(markerAbs)
	if err != nil {
		return nil, err
	}
	var info tcpMarkerInfo
	if err := json.Unmarshal(content, &info); err != nil {
		return nil, err
	}
	fromPID, toPID, ok := parseChannelPIDs(relMarker)
	if !ok {
		return nil, fmt.Errorf("invalid channel name: %s", relMarker)
	}
	info.FromPID = fromPID
	info.ToPID = toPID
	if localEmail != "" && info.Ports != nil {
		if port, ok := info.Ports[localEmail]; ok {
			info.Port = port
		}
	}
	if info.Port == 0 {
		return nil, fmt.Errorf("missing port")
	}
	if strings.TrimSpace(info.From) == "" || strings.TrimSpace(info.To) == "" {
		return nil, fmt.Errorf("missing from/to")
	}
	return &info, nil
}

func parseChannelPIDs(relMarker string) (int, int, bool) {
	parts := strings.Split(filepath.ToSlash(relMarker), "/")
	if len(parts) < 2 {
		return 0, 0, false
	}
	channel := parts[len(parts)-2]
	split := strings.Split(channel, "_to_")
	if len(split) != 2 {
		return 0, 0, false
	}
	from, err1 := strconv.Atoi(split[0])
	to, err2 := strconv.Atoi(split[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return from, to, true
}

func canonicalTCPKey(relMarker string, info *tcpMarkerInfo) string {
	parts := strings.Split(filepath.ToSlash(relMarker), "/")
	if len(parts) < 2 {
		return ""
	}
	if parts[len(parts)-1] != hotlinkTCPMarkerName {
		return ""
	}
	minPID := info.FromPID
	maxPID := info.ToPID
	minEmail := info.From
	if info.ToPID < info.FromPID {
		minPID = info.ToPID
		maxPID = info.FromPID
		minEmail = info.To
	}
	parts[0] = minEmail
	parts[len(parts)-2] = fmt.Sprintf("%d_to_%d", minPID, maxPID)
	parts[len(parts)-1] = hotlinkTCPSuffix
	return strings.Join(parts, "/")
}

func localTCPKey(relMarker string) string {
	parts := strings.Split(filepath.ToSlash(relMarker), "/")
	if len(parts) < 2 {
		return ""
	}
	if parts[len(parts)-1] != hotlinkTCPMarkerName {
		return ""
	}
	parts[len(parts)-1] = hotlinkTCPSuffix
	return strings.Join(parts, "/")
}

func isTCPProxyPath(path string) bool {
	return strings.HasSuffix(path, hotlinkTCPSuffix)
}

func tcpProxyBindIP() string {
	addr := strings.TrimSpace(os.Getenv(hotlinkTCPProxyAddr))
	if addr == "" {
		return "127.0.0.1"
	}
	if strings.Contains(addr, ":") {
		return strings.Split(addr, ":")[0]
	}
	return addr
}

func (h *HotlinkManager) openOutbound(pathKey, relPath string) *hotlinkOutbound {
	sessionID := utils.TokenHex(8)
	out := &hotlinkOutbound{
		id:      sessionID,
		pathKey: pathKey,
		accept:  make(chan struct{}),
		reject:  make(chan string, 1),
	}
	if h.quicEnabled {
		out.quic = &hotlinkQuicOutbound{ready: make(chan struct{})}
	}

	h.outMu.Lock()
	h.outbound[sessionID] = out
	h.outboundByPath[pathKey] = out
	h.outMu.Unlock()

	if err := h.sdk.Events.Send(syftmsg.NewHotlinkOpen(sessionID, relPath)); err != nil {
		h.removeOutbound(sessionID)
		return nil
	}
	return out
}

func (h *HotlinkManager) waitAccepted(out *hotlinkOutbound, timeout time.Duration) bool {
	out.mu.Lock()
	if out.accepted {
		out.mu.Unlock()
		return true
	}
	out.mu.Unlock()

	select {
	case <-out.accept:
		return true
	case <-out.reject:
		return false
	case <-time.After(timeout):
		return false
	}
}

func (h *HotlinkManager) removeOutbound(id string) *hotlinkOutbound {
	h.outMu.Lock()
	out := h.outbound[id]
	if out != nil {
		delete(h.outbound, id)
		if current := h.outboundByPath[out.pathKey]; current == out {
			delete(h.outboundByPath, out.pathKey)
		}
	}
	h.outMu.Unlock()
	return out
}

func isHotlinkEligible(relPath string) bool {
	return strings.HasSuffix(relPath, ".request") || strings.HasSuffix(relPath, ".response")
}

type hotlinkDedupe struct {
	mu    sync.Mutex
	order []string
	set   map[string]struct{}
	max   int
}

func newHotlinkDedupe(max int) *hotlinkDedupe {
	return &hotlinkDedupe{
		order: make([]string, 0, max),
		set:   make(map[string]struct{}, max),
		max:   max,
	}
}

func (d *hotlinkDedupe) Seen(path, etag string) bool {
	if etag == "" {
		return false
	}
	key := path + "|" + etag
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.set[key]; ok {
		return true
	}
	d.set[key] = struct{}{}
	d.order = append(d.order, key)
	if len(d.order) > d.max {
		old := d.order[0]
		d.order = d.order[1:]
		delete(d.set, old)
	}
	return false
}

func encodeHotlinkFrame(path, etag string, seq uint64, payload []byte) []byte {
	pathBytes := []byte(path)
	etagBytes := []byte(etag)
	headerLen := 4 + 1 + 2 + 2 + 4 + 8
	total := headerLen + len(pathBytes) + len(etagBytes) + len(payload)
	buf := bytes.NewBuffer(make([]byte, 0, total))
	buf.WriteString(hotlinkFrameMagic)
	buf.WriteByte(byte(hotlinkFrameVersion))
	_ = binary.Write(buf, binary.BigEndian, uint16(len(pathBytes)))
	_ = binary.Write(buf, binary.BigEndian, uint16(len(etagBytes)))
	_ = binary.Write(buf, binary.BigEndian, uint32(len(payload)))
	_ = binary.Write(buf, binary.BigEndian, seq)
	buf.Write(pathBytes)
	buf.Write(etagBytes)
	buf.Write(payload)
	return buf.Bytes()
}

type hotlinkFrame struct {
	path    string
	etag    string
	seq     uint64
	payload []byte
}

func decodeHotlinkFrame(r *bufio.Reader) (*hotlinkFrame, error) {
	magic := []byte(hotlinkFrameMagic)
	window := make([]byte, 0, len(magic))

	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		window = append(window, b)
		if len(window) > len(magic) {
			window = window[1:]
		}
		if len(window) < len(magic) || !bytes.Equal(window, magic) {
			continue
		}

		header := make([]byte, 1+2+2+4+8)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, err
		}
		if header[0] != hotlinkFrameVersion {
			window = window[:0]
			continue
		}
		pathLen := binary.BigEndian.Uint16(header[1:3])
		etagLen := binary.BigEndian.Uint16(header[3:5])
		payloadLen := binary.BigEndian.Uint32(header[5:9])
		seq := binary.BigEndian.Uint64(header[9:17])

		frame := &hotlinkFrame{seq: seq}
		if pathLen > 0 {
			path := make([]byte, pathLen)
			if _, err := io.ReadFull(r, path); err != nil {
				return nil, err
			}
			frame.path = string(path)
		}
		if etagLen > 0 {
			etag := make([]byte, etagLen)
			if _, err := io.ReadFull(r, etag); err != nil {
				return nil, err
			}
			frame.etag = string(etag)
		}
		if payloadLen > 0 {
			frame.payload = make([]byte, payloadLen)
			if _, err := io.ReadFull(r, frame.payload); err != nil {
				return nil, err
			}
		}
		return frame, nil
	}
}

func acceptHotlinkConn(listener net.Listener, timeout time.Duration) (net.Conn, error) {
	if listener == nil {
		return nil, fmt.Errorf("hotlink listener not available")
	}
	if dl, ok := listener.(interface{ SetDeadline(time.Time) error }); ok {
		_ = dl.SetDeadline(time.Now().Add(timeout))
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, fmt.Errorf("timeout waiting for hotlink ipc connection")
			}
			return nil, err
		}
		return conn, nil
	}

	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := listener.Accept()
		ch <- result{conn: conn, err: err}
	}()
	select {
	case res := <-ch:
		return res.conn, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for hotlink ipc connection")
	}
}

func quicOfferAddrs(addr string, stunAddr string) []string {
	addrs := []string{}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host == "" || host == "0.0.0.0" || host == "::" {
			addrs = append(addrs, fmt.Sprintf("127.0.0.1:%s", port))
			bindIP := tcpProxyBindIP()
			if bindIP != "" && bindIP != "127.0.0.1" {
				addrs = append(addrs, fmt.Sprintf("%s:%s", bindIP, port))
			}
		} else {
			addrs = append(addrs, addr)
		}
	}
	if stunAddr != "" {
		addrs = appendUniqueAddr(addrs, stunAddr)
	}
	if len(addrs) == 0 {
		addrs = append(addrs, addr)
	}
	return addrs
}

func appendUniqueAddr(addrs []string, addr string) []string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addrs
	}
	for _, existing := range addrs {
		if strings.EqualFold(strings.TrimSpace(existing), addr) {
			return addrs
		}
	}
	return append(addrs, addr)
}

func discoverStunAddr(conn *net.UDPConn) (string, error) {
	if conn == nil {
		return "", fmt.Errorf("udp connection not available")
	}

	server := strings.TrimSpace(os.Getenv(hotlinkStunServerEnv))
	if server == "" {
		server = "stun.l.google.com:19302"
	}
	if server == "0" || strings.EqualFold(server, "off") || strings.EqualFold(server, "disabled") {
		return "", nil
	}

	serverAddr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return "", err
	}

	var txID [12]byte
	if _, err := rand.Read(txID[:]); err != nil {
		return "", err
	}

	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:2], 0x0001) // Binding request
	binary.BigEndian.PutUint16(req[2:4], 0x0000) // No attributes
	binary.BigEndian.PutUint32(req[4:8], 0x2112A442)
	copy(req[8:], txID[:])

	_ = conn.SetWriteDeadline(time.Now().Add(hotlinkStunTimeout))
	if _, err := conn.WriteToUDP(req, serverAddr); err != nil {
		_ = conn.SetDeadline(time.Time{})
		return "", err
	}

	resp := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(hotlinkStunTimeout))
	n, _, err := conn.ReadFromUDP(resp)
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		return "", err
	}

	addr, err := parseStunMappedAddr(resp[:n], txID)
	if err != nil {
		return "", err
	}
	if addr == nil {
		return "", fmt.Errorf("no mapped address in stun response")
	}
	return addr.String(), nil
}

func parseStunMappedAddr(msg []byte, txID [12]byte) (*net.UDPAddr, error) {
	if len(msg) < 20 {
		return nil, fmt.Errorf("stun response too short")
	}
	if binary.BigEndian.Uint16(msg[0:2]) != 0x0101 {
		return nil, fmt.Errorf("unexpected stun response type")
	}
	if binary.BigEndian.Uint32(msg[4:8]) != 0x2112A442 {
		return nil, fmt.Errorf("invalid stun magic cookie")
	}
	if !bytes.Equal(msg[8:20], txID[:]) {
		return nil, fmt.Errorf("stun transaction mismatch")
	}

	msgLen := int(binary.BigEndian.Uint16(msg[2:4]))
	limit := 20 + msgLen
	if limit > len(msg) {
		limit = len(msg)
	}
	offset := 20
	for offset+4 <= limit {
		typ := binary.BigEndian.Uint16(msg[offset : offset+2])
		l := int(binary.BigEndian.Uint16(msg[offset+2 : offset+4]))
		offset += 4
		if offset+l > limit {
			break
		}
		value := msg[offset : offset+l]
		switch typ {
		case 0x0020: // XOR-MAPPED-ADDRESS
			if addr, err := parseStunAddressValue(value, txID, true); err == nil {
				return addr, nil
			}
		case 0x0001: // MAPPED-ADDRESS
			if addr, err := parseStunAddressValue(value, txID, false); err == nil {
				return addr, nil
			}
		}
		offset += l
		if rem := offset % 4; rem != 0 {
			offset += 4 - rem
		}
	}
	return nil, fmt.Errorf("no mapped address attributes")
}

func parseStunAddressValue(value []byte, txID [12]byte, xor bool) (*net.UDPAddr, error) {
	if len(value) < 8 {
		return nil, fmt.Errorf("stun address attribute too short")
	}
	family := value[1]
	port := binary.BigEndian.Uint16(value[2:4])

	switch family {
	case 0x01: // IPv4
		ip := make(net.IP, net.IPv4len)
		copy(ip, value[4:8])
		if xor {
			port ^= uint16(0x2112A442 >> 16)
			ip[0] ^= 0x21
			ip[1] ^= 0x12
			ip[2] ^= 0xA4
			ip[3] ^= 0x42
		}
		return &net.UDPAddr{IP: ip, Port: int(port)}, nil
	case 0x02: // IPv6
		if len(value) < 20 {
			return nil, fmt.Errorf("stun ipv6 attribute too short")
		}
		ip := make(net.IP, net.IPv6len)
		copy(ip, value[4:20])
		if xor {
			port ^= uint16(0x2112A442 >> 16)
			mask := make([]byte, 16)
			copy(mask[0:4], []byte{0x21, 0x12, 0xA4, 0x42})
			copy(mask[4:16], txID[:])
			for i := 0; i < 16; i++ {
				ip[i] ^= mask[i]
			}
		}
		return &net.UDPAddr{IP: ip, Port: int(port)}, nil
	default:
		return nil, fmt.Errorf("unsupported stun family %d", family)
	}
}

func newQuicServerTLSConfig() (*tls.Config, error) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{hotlinkQuicALPN},
	}, nil
}

func newQuicClientTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{hotlinkQuicALPN},
	}
}

func generateSelfSignedCert() (tls.Certificate, error) {
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := &bytes.Buffer{}
	keyPEM := &bytes.Buffer{}
	if err := pem.Encode(certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return tls.Certificate{}, err
	}
	if err := pem.Encode(keyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM.Bytes(), keyPEM.Bytes())
}

func writeQuicHandshake(stream *quic.Stream, sessionID string) error {
	if len(sessionID) > 0xffff {
		return fmt.Errorf("session id too long")
	}
	buf := bytes.NewBuffer(nil)
	buf.WriteString("HLQ1")
	if err := binary.Write(buf, binary.BigEndian, uint16(len(sessionID))); err != nil {
		return err
	}
	buf.WriteString(sessionID)
	_, err := stream.Write(buf.Bytes())
	return err
}

func readQuicHandshake(r *bufio.Reader, sessionID string) error {
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return err
	}
	if string(magic) != "HLQ1" {
		return fmt.Errorf("invalid quic handshake magic")
	}
	var l uint16
	if err := binary.Read(r, binary.BigEndian, &l); err != nil {
		return err
	}
	if l == 0 {
		return fmt.Errorf("invalid quic handshake length")
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	if string(buf) != sessionID {
		return fmt.Errorf("quic handshake session mismatch")
	}
	return nil
}
