package sync

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	hotlinkEnabledEnv     = "SYFTBOX_HOTLINK"
	hotlinkSocketOnlyEnv  = "SYFTBOX_HOTLINK_SOCKET_ONLY"
	hotlinkAcceptName     = "stream.accept"
	hotlinkAcceptDelay    = 200 * time.Millisecond
	hotlinkAcceptTimeout  = 1500 * time.Millisecond
	hotlinkFrameMagic     = "HLNK"
	hotlinkFrameVersion   = 1
	hotlinkDedupeMax      = 1024
	hotlinkConnectTimeout = 5 * time.Second
)

type hotlinkSession struct {
	id         string
	path       string
	dirAbs     string
	ipcPath    string
	acceptPath string
	done       chan struct{}
}

type hotlinkOutbound struct {
	id       string
	pathKey  string
	accept   chan struct{}
	reject   chan string
	seq      uint64
	accepted bool
	mu       sync.Mutex
}

type HotlinkManager struct {
	workspace  *workspace.Workspace
	sdk        *syftsdk.SyftSDK
	enabled    bool
	socketOnly bool

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
}

func NewHotlinkManager(ws *workspace.Workspace, sdk *syftsdk.SyftSDK) *HotlinkManager {
	return &HotlinkManager{
		workspace:      ws,
		sdk:            sdk,
		enabled:        os.Getenv(hotlinkEnabledEnv) == "1",
		socketOnly:     os.Getenv(hotlinkSocketOnlyEnv) == "1",
		sessions:       make(map[string]*hotlinkSession),
		outbound:       make(map[string]*hotlinkOutbound),
		outboundByPath: make(map[string]*hotlinkOutbound),
		dedupe:         newHotlinkDedupe(hotlinkDedupeMax),
		ipcWriters:     make(map[string]*hotlinkIPC),
		localReaders:   make(map[string]*hotlinkLocalReader),
	}
}

func (h *HotlinkManager) Enabled() bool {
	return h.enabled
}

func (h *HotlinkManager) SocketOnly() bool {
	return h.socketOnly
}

func (h *HotlinkManager) StartLocalReaders(ctx context.Context) {
	if !h.enabled || !h.socketOnly {
		return
	}
	go h.scanLocalReaders(ctx)
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
	root := filepath.Join(h.workspace.UserDir, "app_data")
	pattern := filepath.Join(root, "*", "rpc", "*", hotlinkIPCMarkerName())
	paths, err := filepath.Glob(pattern)
	if err != nil || len(paths) == 0 {
		return
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

	if utils.FileExists(session.acceptPath) {
		if err := h.sdk.Events.Send(syftmsg.NewHotlinkAccept(session.id)); err != nil {
			slog.Warn("hotlink accept send failed", "session", session.id, "error", err)
		}
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

	writer := h.getIPCWriter(session.ipcPath)
	if writer == nil {
		return
	}
	if len(data.Payload) == 0 {
		return
	}

	etag := strings.TrimSpace(data.ETag)
	if etag == "" {
		etag = fmt.Sprintf("%x", md5.Sum(data.Payload))
	}
	if h.dedupe.Seen(session.path, etag) {
		return
	}

	framePath := session.path
	if strings.TrimSpace(data.Path) != "" {
		framePath = data.Path
	}
	frame := encodeHotlinkFrame(framePath, etag, data.Seq, data.Payload)
	if err := writer.Write(frame); err != nil {
		slog.Warn("hotlink ipc write failed", "session", session.id, "error", err)
	} else {
		slog.Debug("hotlink ipc wrote", "session", session.id, "bytes", len(frame))
		if latencyTraceEnabled() {
			if ts, ok := payloadTimestampNs(data.Payload); ok {
				slog.Info("latency_trace hotlink_ipc_written", "path", framePath, "age_ms", (time.Now().UnixNano()-ts)/1_000_000, "size", len(data.Payload))
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
	go h.sendHotlink(relPath, etag, payload)
}

func (h *HotlinkManager) sendHotlink(relPath string, etag string, payload []byte) {
	pathKey := filepath.Dir(relPath)
	out := h.getOrOpenOutbound(pathKey, relPath)
	if out == nil {
		return
	}

	if !h.waitAccepted(out, hotlinkAcceptTimeout) {
		_ = h.sdk.Events.Send(syftmsg.NewHotlinkClose(out.id, "fallback"))
		h.removeOutbound(out.id)
		return
	}

	out.mu.Lock()
	out.seq++
	seq := out.seq
	out.mu.Unlock()
	if err := h.sdk.Events.Send(syftmsg.NewHotlinkData(out.id, seq, relPath, etag, payload)); err != nil {
		_ = h.sdk.Events.Send(syftmsg.NewHotlinkClose(out.id, "fallback"))
		h.removeOutbound(out.id)
		return
	}
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

func (h *HotlinkManager) openOutbound(pathKey, relPath string) *hotlinkOutbound {
	sessionID := utils.TokenHex(8)
	out := &hotlinkOutbound{
		id:      sessionID,
		pathKey: pathKey,
		accept:  make(chan struct{}),
		reject:  make(chan string, 1),
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
