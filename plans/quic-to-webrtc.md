# QUIC → WebRTC Migration Guide

## Overview

Replace the custom QUIC+STUN P2P transport with WebRTC data channels. WebRTC's ICE handles NAT traversal automatically (~95% success rate). The existing WS connection to the server acts as the signaling channel. **No server changes needed.**

### Why

| | QUIC (old) | WebRTC (new) |
|--|-----------|-------------|
| NAT traversal | Manual STUN + direct dial (LAN only) | Automatic ICE (STUN + TURN) |
| Symmetric NAT | Fails | Works via TURN relay |
| TLS/certs | Self-signed RSA 2048, manual | Built-in DTLS, automatic |
| Connection | Direct UDP dial to address list | ICE negotiates best path |
| Setup code | ~324 lines (Rust), ~200 lines (Go) | ~30 lines |
| Dependencies | quinn + rcgen + rustls / quic-go | single webrtc crate / pion |
| Read model | Stream-based read loop | Message-oriented callbacks |

### What Stays the Same

- All 6 hotlink message types (Open/Accept/Reject/Data/Close/Signal)
- HotlinkSignal (type 14) — same fields, different `kind` values
- Server relay — `handleHotlinkSignal` is transport-agnostic
- TCP proxy discovery and marker files
- Reorder buffer
- IPC framing format (`HLNK` header)
- WS fallback behavior

---

## Go Client: Step-by-Step

### Step 1: Update Dependencies

```bash
# Remove
go get -u github.com/quic-go/quic-go@none

# Add
go get github.com/pion/webrtc/v4
```

### Step 2: Remove QUIC Code

Delete from `internal/client/sync/hotlink_manager.go`:

| What | Lines | Description |
|------|-------|-------------|
| `hotlinkQuicSession` struct | 81-90 | QUIC listener/conn/stream holder |
| `hotlinkQuicOutbound` struct | 92-99 | QUIC outbound conn/stream holder |
| `quic` field in `hotlinkSession` | 66 | Remove field |
| `quic` field in `hotlinkOutbound` | 78 | Remove field |
| `quicEnabled` field in `HotlinkManager` | 107 | Remove field |
| `quicOnly` field in `HotlinkManager` | 108 | Remove field |
| `maybeStartQuicOffer()` | 614-661 | QUIC server setup + offer |
| `acceptQuic()` | 663-710 | QUIC accept + handshake |
| `handleQuicOffer()` | 712-773 | QUIC client dial + answer |
| `handleQuicAnswer()` | 775-790 | QUIC answer handling |
| `trySendQuic()` | 792-828 | QUIC stream send |
| `runQuicReader()` | 830-844 | QUIC stream read loop |
| `newQuicServerTLSConfig()` | 1633-1642 | Self-signed TLS config |
| `newQuicClientTLSConfig()` | 1644-1649 | InsecureSkipVerify config |
| `generateSelfSignedCert()` | 1651-1680 | RSA cert generation |
| `writeQuicHandshake()` | 1682-1694 | HLQ1 handshake write |
| `readQuicHandshake()` | 1696-1719 | HLQ1 handshake read |

Remove constants:

| Constant | Line | Description |
|----------|------|-------------|
| `hotlinkQuicEnv` | 42 | `SYFTBOX_HOTLINK_QUIC` |
| `hotlinkQuicOnlyEnv` | 43 | `SYFTBOX_HOTLINK_QUIC_ONLY` |
| `hotlinkStunServerEnv` | 41 | `SYFTBOX_HOTLINK_STUN_SERVER` |
| `hotlinkQuicDialTimeout` | 47 | 1500ms |
| `hotlinkQuicAcceptTimeout` | 48 | 2500ms |
| `hotlinkQuicALPN` | 56 | `"syftbox-hotlink"` |

Remove imports:

```go
// Remove:
"github.com/quic-go/quic-go"
"crypto/tls"      // if only used for QUIC
"crypto/x509"     // if only used for QUIC
"crypto/rsa"      // if only used for QUIC
"crypto/rand"     // if only used for QUIC cert gen
"math/big"        // if only used for QUIC cert gen
```

### Step 3: Add WebRTC Struct

Replace `hotlinkQuicSession`/`hotlinkQuicOutbound` with a single struct:

```go
import (
    "github.com/pion/webrtc/v4"
    "sync"
    "sync/atomic"
)

type webrtcSession struct {
    pc              *webrtc.PeerConnection
    dc              *webrtc.DataChannel
    ready           chan struct{}
    readyFlag       atomic.Bool
    err             error
    pendingCands    []webrtc.ICECandidateInit
    remoteDescSet   atomic.Bool
    mu              sync.Mutex // protects pendingCands and err
}
```

Update `hotlinkSession` and `hotlinkOutbound`:

```go
type hotlinkSession struct {
    // ... existing fields ...
    webrtc *webrtcSession  // was: quic *hotlinkQuicSession
}

type hotlinkOutbound struct {
    // ... existing fields ...
    webrtc *webrtcSession  // was: quic *hotlinkQuicOutbound
}
```

### Step 4: Add ICE Server Config

```go
const (
    hotlinkICEServersEnv = "SYFTBOX_HOTLINK_ICE_SERVERS"
    hotlinkTurnUserEnv   = "SYFTBOX_HOTLINK_TURN_USER"
    hotlinkTurnPassEnv   = "SYFTBOX_HOTLINK_TURN_PASS"
    hotlinkWebRTCTimeout = 10 * time.Second
)

func iceServers() []webrtc.ICEServer {
    raw := os.Getenv(hotlinkICEServersEnv)
    if raw == "" {
        raw = os.Getenv("SYFTBOX_HOTLINK_STUN_SERVER") // backwards compat
    }
    if raw == "" {
        raw = "stun:stun.l.google.com:19302"
    }
    user := os.Getenv(hotlinkTurnUserEnv)
    pass := os.Getenv(hotlinkTurnPassEnv)

    var servers []webrtc.ICEServer
    for _, u := range strings.Split(raw, ",") {
        u = strings.TrimSpace(u)
        if u == "" {
            continue
        }
        s := webrtc.ICEServer{URLs: []string{u}}
        if strings.HasPrefix(u, "turn:") || strings.HasPrefix(u, "turns:") {
            s.Username = user
            s.Credential = pass
        }
        servers = append(servers, s)
    }
    return servers
}
```

### Step 5: Add Session Factory

```go
func createWebRTCSession() (*webrtcSession, error) {
    se := webrtc.SettingEngine{}
    // Optional: se.DetachDataChannels() if you want raw read/write

    api := webrtc.NewAPI(
        webrtc.WithSettingEngine(se),
    )

    pc, err := api.NewPeerConnection(webrtc.Configuration{
        ICEServers: iceServers(),
    })
    if err != nil {
        return nil, fmt.Errorf("webrtc new peer connection: %w", err)
    }

    return &webrtcSession{
        pc:    pc,
        ready: make(chan struct{}),
    }, nil
}
```

### Step 6: Update Signal Dispatch

Replace the `HandleSignal` switch (lines 519-534):

```go
func (h *HotlinkManager) HandleSignal(signal syftmsg.HotlinkSignal) {
    if !h.enabled {
        return
    }
    switch signal.Kind {
    case "sdp_offer":
        go h.handleSDPOffer(signal)
    case "sdp_answer":
        h.handleSDPAnswer(signal)
    case "ice_candidate":
        h.handleICECandidate(signal)
    case "webrtc_error":
        log.Printf("hotlink webrtc error: session=%s error=%s", signal.SessionID, signal.Error)
    case "quic_offer", "quic_answer", "quic_error":
        // backwards compat: ignore old signals from peers not yet upgraded
    default:
        // ignore unknown
    }
}
```

### Step 7: Implement Offer (replaces `maybeStartQuicOffer`)

Called after receiving `HotlinkAccept`:

```go
func (h *HotlinkManager) startWebRTCOffer(sessionID string) {
    sess, err := createWebRTCSession()
    if err != nil {
        log.Printf("hotlink webrtc session create failed: %v", err)
        return
    }

    // ICE candidate trickle
    sess.pc.OnICECandidate(func(c *webrtc.ICECandidate) {
        if c == nil {
            return
        }
        candidateJSON, _ := json.Marshal(c.ToJSON())
        h.sendSignal(sessionID, "ice_candidate", string(candidateJSON), "")
    })

    // Create data channel (offerer creates it)
    ordered := true
    dc, err := sess.pc.CreateDataChannel("syftbox-data", &webrtc.DataChannelInit{
        Ordered: &ordered,
    })
    if err != nil {
        log.Printf("hotlink webrtc create data channel failed: %v", err)
        sess.pc.Close()
        return
    }
    sess.dc = dc

    // Data channel open
    dc.OnOpen(func() {
        log.Printf("hotlink webrtc data channel open (offerer): session=%s", sessionID)
        sess.readyFlag.Store(true)
        close(sess.ready)
    })

    // Receive data
    dc.OnMessage(func(msg webrtc.DataChannelMessage) {
        h.handleWebRTCMessage(sessionID, msg.Data)
    })

    // Store session
    h.mu.Lock()
    if s, ok := h.sessions[sessionID]; ok {
        s.webrtc = sess
    }
    if o, ok := h.outbound[sessionID]; ok {
        o.webrtc = sess
    }
    h.mu.Unlock()

    // Create and send SDP offer
    offer, err := sess.pc.CreateOffer(nil)
    if err != nil {
        log.Printf("hotlink webrtc create offer failed: %v", err)
        return
    }
    if err := sess.pc.SetLocalDescription(offer); err != nil {
        log.Printf("hotlink webrtc set local desc failed: %v", err)
        return
    }

    offerJSON, _ := json.Marshal(offer)
    h.sendSignal(sessionID, "sdp_offer", string(offerJSON), "")
}
```

### Step 8: Implement SDP Answer (replaces `handleQuicOffer`)

```go
func (h *HotlinkManager) handleSDPOffer(signal syftmsg.HotlinkSignal) {
    sessionID := signal.SessionID

    sess, err := createWebRTCSession()
    if err != nil {
        log.Printf("hotlink webrtc session create failed: %v", err)
        h.sendSignal(sessionID, "webrtc_error", "", err.Error())
        return
    }

    // ICE candidate trickle
    sess.pc.OnICECandidate(func(c *webrtc.ICECandidate) {
        if c == nil {
            return
        }
        candidateJSON, _ := json.Marshal(c.ToJSON())
        h.sendSignal(sessionID, "ice_candidate", string(candidateJSON), "")
    })

    // Receive data channel from offerer
    sess.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
        sess.dc = dc
        dc.OnOpen(func() {
            log.Printf("hotlink webrtc data channel open (answerer): session=%s", sessionID)
            sess.readyFlag.Store(true)
            close(sess.ready)
        })
        dc.OnMessage(func(msg webrtc.DataChannelMessage) {
            h.handleWebRTCMessage(sessionID, msg.Data)
        })
    })

    // Store session
    h.mu.Lock()
    if s, ok := h.sessions[sessionID]; ok {
        s.webrtc = sess
    }
    if o, ok := h.outbound[sessionID]; ok {
        o.webrtc = sess
    }
    h.mu.Unlock()

    // Set remote description (the offer)
    var offer webrtc.SessionDescription
    if err := json.Unmarshal([]byte(signal.Token), &offer); err != nil {
        log.Printf("hotlink webrtc unmarshal offer failed: %v", err)
        return
    }
    if err := sess.pc.SetRemoteDescription(offer); err != nil {
        log.Printf("hotlink webrtc set remote desc failed: %v", err)
        h.sendSignal(sessionID, "webrtc_error", "", err.Error())
        return
    }
    sess.remoteDescSet.Store(true)

    // Flush buffered ICE candidates
    sess.mu.Lock()
    for _, c := range sess.pendingCands {
        _ = sess.pc.AddICECandidate(c)
    }
    sess.pendingCands = nil
    sess.mu.Unlock()

    // Create and send answer
    answer, err := sess.pc.CreateAnswer(nil)
    if err != nil {
        log.Printf("hotlink webrtc create answer failed: %v", err)
        return
    }
    if err := sess.pc.SetLocalDescription(answer); err != nil {
        log.Printf("hotlink webrtc set local desc failed: %v", err)
        return
    }

    answerJSON, _ := json.Marshal(answer)
    h.sendSignal(sessionID, "sdp_answer", string(answerJSON), "")
}
```

### Step 9: Implement SDP Answer Handler (replaces `handleQuicAnswer`)

```go
func (h *HotlinkManager) handleSDPAnswer(signal syftmsg.HotlinkSignal) {
    sessionID := signal.SessionID

    h.mu.RLock()
    s, ok := h.sessions[sessionID]
    h.mu.RUnlock()
    if !ok || s.webrtc == nil {
        return
    }
    sess := s.webrtc

    var answer webrtc.SessionDescription
    if err := json.Unmarshal([]byte(signal.Token), &answer); err != nil {
        log.Printf("hotlink webrtc unmarshal answer failed: %v", err)
        return
    }
    if err := sess.pc.SetRemoteDescription(answer); err != nil {
        log.Printf("hotlink webrtc set remote desc failed: %v", err)
        return
    }
    sess.remoteDescSet.Store(true)

    // Flush buffered ICE candidates
    sess.mu.Lock()
    for _, c := range sess.pendingCands {
        _ = sess.pc.AddICECandidate(c)
    }
    sess.pendingCands = nil
    sess.mu.Unlock()
}
```

### Step 10: Implement ICE Candidate Handler (new)

```go
func (h *HotlinkManager) handleICECandidate(signal syftmsg.HotlinkSignal) {
    sessionID := signal.SessionID

    // Find session (check both sessions and outbound)
    var sess *webrtcSession
    h.mu.RLock()
    if s, ok := h.sessions[sessionID]; ok && s.webrtc != nil {
        sess = s.webrtc
    } else if o, ok := h.outbound[sessionID]; ok && o.webrtc != nil {
        sess = o.webrtc
    }
    h.mu.RUnlock()

    if sess == nil {
        return
    }

    var candidate webrtc.ICECandidateInit
    if err := json.Unmarshal([]byte(signal.Token), &candidate); err != nil {
        log.Printf("hotlink webrtc unmarshal ice candidate failed: %v", err)
        return
    }

    // Buffer if remote description not yet set
    if !sess.remoteDescSet.Load() {
        sess.mu.Lock()
        sess.pendingCands = append(sess.pendingCands, candidate)
        sess.mu.Unlock()
        return
    }

    // Apply immediately
    if err := sess.pc.AddICECandidate(candidate); err != nil {
        log.Printf("hotlink webrtc add ice candidate failed: %v", err)
    }
}
```

### Step 11: Implement Send (replaces `trySendQuic`)

```go
func (h *HotlinkManager) trySendWebRTC(out *hotlinkOutbound, relPath, etag string, seq uint64, payload []byte, wait bool) (bool, error) {
    if out == nil || out.webrtc == nil {
        return false, nil
    }
    sess := out.webrtc

    // Wait for ready if requested
    if !sess.readyFlag.Load() {
        if wait {
            select {
            case <-sess.ready:
            case <-time.After(hotlinkWebRTCTimeout):
                return false, fmt.Errorf("webrtc wait timeout")
            }
        } else {
            return false, nil
        }
    }

    // Check for error
    sess.mu.Lock()
    err := sess.err
    sess.mu.Unlock()
    if err != nil {
        return false, err
    }

    if sess.dc == nil {
        return false, nil
    }

    // Encode HLNK frame and send
    frame := encodeHotlinkFrame(relPath, etag, seq, payload)
    if err := sess.dc.Send(frame); err != nil {
        return false, fmt.Errorf("webrtc send: %w", err)
    }
    return true, nil
}
```

### Step 12: Implement Message Handler (replaces `runQuicReader`)

```go
func (h *HotlinkManager) handleWebRTCMessage(sessionID string, data []byte) {
    frame, err := parseHotlinkFrame(data)
    if err != nil {
        log.Printf("hotlink webrtc frame parse failed: %v", err)
        return
    }

    h.mu.RLock()
    session, ok := h.sessions[sessionID]
    h.mu.RUnlock()
    if !ok {
        return
    }

    h.handleHotlinkPayload(session, frame.path, frame.etag, frame.seq, frame.payload)
}
```

### Step 13: Update `sendHotlink` Call Site

Wherever `trySendQuic` was called, replace with `trySendWebRTC`:

```go
// Old:
sent, err := h.trySendQuic(out, relPath, etag, seq, payload, wait)

// New:
sent, err := h.trySendWebRTC(out, relPath, etag, seq, payload, wait)
```

The WS fallback logic stays identical:
```go
if sent {
    h.telemetry.txP2PPackets++
    return nil
}
// fall through to WS send
```

### Step 14: Update `HotlinkManager` Init

```go
// Old:
quicEnabled: strings.TrimSpace(os.Getenv(hotlinkQuicEnv)) != "0",
quicOnly:    os.Getenv(hotlinkQuicOnlyEnv) == "1",

// New: remove these fields entirely. WebRTC is always on when hotlink is enabled.
```

### Step 15: Update Accept Handler

Where `maybeStartQuicOffer` was called after `HotlinkAccept`:

```go
// Old:
if h.quicEnabled {
    go h.maybeStartQuicOffer(session)
}

// New:
go h.startWebRTCOffer(session.id)
```

### Step 16: Clean Up Env Vars

| Remove | Replacement |
|--------|-------------|
| `SYFTBOX_HOTLINK_QUIC` | (removed — always on) |
| `SYFTBOX_HOTLINK_QUIC_ONLY` | (removed) |
| `SYFTBOX_HOTLINK_STUN_SERVER` | `SYFTBOX_HOTLINK_ICE_SERVERS` (backwards compat reads old var) |

New env vars:

| Variable | Default | Purpose |
|----------|---------|---------|
| `SYFTBOX_HOTLINK_ICE_SERVERS` | `stun:stun.l.google.com:19302` | Comma-separated ICE server URLs |
| `SYFTBOX_HOTLINK_TURN_USER` | (empty) | TURN credentials |
| `SYFTBOX_HOTLINK_TURN_PASS` | (empty) | TURN credentials |

---

## Verification

### 1. Build
```bash
go build ./...
go vet ./...
```

### 2. E2E Test (local, no Docker)
```bash
just sbdev-test-single TestHotlinkTCPProxy mode=go
```
Expects: 20 chunks, 81920 bytes, correct order.

### 3. NAT Traversal Test (Docker)
```bash
bash docker/nat-test.sh
```
Expects: "WebRTC data channels open on BOTH sides through NAT (via TURN relay)"

### 4. Telemetry Check
After E2E test, check `{datasite}/.syftbox/hotlink_telemetry.json`:
- `tx_p2p_packets > 0` → WebRTC is working
- `tx_ws_packets` should be low (only initial frames before WebRTC ready)

### 5. Cross-Client Compatibility
Run with one Rust client and one Go client to verify interop:
```bash
just sbdev-test-single TestHotlinkTCPProxy mode=mixed
```

### 6. Distributed Scenario
```bash
cd biovault && go run ./cmd/devstack scenario run syqure-distributed.yaml
```
Expects: MPC result `[3,3,4]` correct across all 3 parties.

---

## Signaling Protocol Reference

All signaling uses `HotlinkSignal` (type 14) over WebSocket. The server is a pure relay — it forwards any `kind` value without inspection.

### Signal Flow: Offer Side (initiator)

```
1. HotlinkAccept received
2. createWebRTCSession()
3. pc.CreateDataChannel("syftbox-data")
4. Register: OnICECandidate → sendSignal("ice_candidate")
5. Register: dc.OnOpen → set ready
6. Register: dc.OnMessage → handleWebRTCMessage()
7. pc.CreateOffer()
8. pc.SetLocalDescription(offer)
9. sendSignal("sdp_offer", offerJSON)
10. Wait for ready (data channel opens after ICE completes)
```

### Signal Flow: Answer Side

```
1. Receive HotlinkSignal(kind="sdp_offer")
2. createWebRTCSession()
3. Register: OnICECandidate → sendSignal("ice_candidate")
4. Register: pc.OnDataChannel → dc.OnOpen → set ready
5. pc.SetRemoteDescription(offer)
6. remoteDescSet = true
7. Flush pendingCandidates → pc.AddICECandidate()
8. pc.CreateAnswer()
9. pc.SetLocalDescription(answer)
10. sendSignal("sdp_answer", answerJSON)
```

### ICE Candidate Flow (both sides, concurrent with above)

```
1. Receive HotlinkSignal(kind="ice_candidate")
2. If remoteDescSet: pc.AddICECandidate(candidate)
3. Else: buffer in pendingCandidates (flushed after SetRemoteDescription)
```

### Token Format

| kind | `tok` field content |
|------|-------------------|
| `sdp_offer` | `{"type":"offer","sdp":"v=0\r\n..."}` |
| `sdp_answer` | `{"type":"answer","sdp":"v=0\r\n..."}` |
| `ice_candidate` | `{"candidate":"candidate:...","sdpMid":"0","sdpMLineIndex":0}` |
| `webrtc_error` | (empty — error goes in `err` field) |

---

## Common Pitfalls

1. **ICE candidate race:** Candidates arrive before `SetRemoteDescription`. Must buffer in `pendingCandidates` and flush after setting remote desc. This is the #1 cause of "ICE failed" errors.

2. **Data channel creation asymmetry:** Only the **offerer** calls `CreateDataChannel()`. The **answerer** receives it via `OnDataChannel` callback. If both sides create a data channel, you get two channels and confusion.

3. **Close cleanup:** Call `pc.Close()` when the hotlink session closes. This releases ICE agents, DTLS, and SCTP resources. Leaking PeerConnections will leak goroutines/threads.

4. **Ordered delivery:** Set `Ordered: true` on the data channel. Without it, SCTP may deliver messages out of order, which would corrupt TCP proxy streams (even with the reorder buffer, since it expects sequential frame numbers, not duplicate deliveries).

5. **Message size:** SCTP default max message size is ~64KB. Our TCP proxy reads 64KB chunks, which fits. If you increase chunk size, you may need to configure SCTP parameters.

6. **Thread safety:** `OnICECandidate`, `OnDataChannel`, `OnOpen`, `OnMessage` callbacks fire from pion's internal goroutines. Access to shared state (session maps, data channel reference) must be synchronized.
