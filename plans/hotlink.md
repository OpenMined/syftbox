# Hotlink Plan

## Goals
- Provide the **lowest-latency** data path between peers for time-critical MPC/HE workloads.
- Keep existing file-based durability as an optional fallback.
- Keep the **application interface stable** (UNIX socket / TCP port), even if hotlink drops.
- Support both Go and Rust SyftBox clients.
- Enable a **TCP tunnel** mode for Syqure-style socket streams (proxied over hotlink).

---

## Architecture

### How It Works (TCP Proxy Mode, End-to-End)

```
Sequre CP1                  SyftBox Rust Client A         SyftBox Go Server          SyftBox Rust Client B         Sequre CP2
  |                              |                              |                              |                        |
  |  1. TCP connect :10001 ----->|                              |                              |                        |
  |                              |  2. HotlinkOpen(path) ------>|                              |                        |
  |                              |                              |  3. ACL check + broadcast --->|                        |
  |                              |                              |                              |  4. bind TCP :10003     |
  |                              |                              |<--- 5. HotlinkAccept(sid) ---|                        |
  |                              |<--- 6. HotlinkAccept(sid) --|                              |                        |
  |                              |                              |                              |                        |
  |                              |===== 7. SDP offer/answer + ICE (HotlinkSignal via WS) ===>|                        |
  |                              |<================ 8. WebRTC data channel established ======>|                        |
  |                              |                              |                              |                        |
  |  9. TCP send(data) -------->|                              |                              |                        |
  |                              | 10. HotlinkData(seq=N) via WebRTC ======================>|                        |
  |                              |                              |                              | 11. reorder → TCP ---->|
  |                              |                              |                              |                        | 12. recv(data)
```

If WebRTC fails or is disabled, steps 7-8 are skipped and step 10 goes via WebSocket through the server.

**Connection upgrade strategy (ICE):**
```
1. Direct data channel (P2P, ~60% of NATs)       ← fastest
2. STUN hole punch via ICE (additional ~30%)      ← still P2P
3. TURN relay on server (symmetric NAT, ~5-8%)    ← server-relayed but lighter than WS
4. WS relay (current fallback, always works)      ← heaviest overhead
```

### Signaling Flow (WebRTC over existing WS)

```
Alice                     Server                    Bob
  |  HotlinkSignal         |                         |
  |  (SDP offer) --------->|  forward SDP --------->|
  |                         |                         |  create answer
  |                         |<------- SDP answer ----|
  |<------ SDP answer -----|                         |
  |                         |                         |
  |  ICE candidates ------->|  forward candidates -->|
  |<------ candidates ------|<------ candidates -----|
  |                         |                         |
  |=========== data channel established ============>|
  |  TCP proxy data flows over data channel          |
```

### Protocol Messages

| Type | ID | Fields | Purpose |
|------|----|--------|---------|
| `HotlinkOpen` | 9 | `sid`, `pth` | Sender opens session for a path |
| `HotlinkAccept` | 10 | `sid` | Receiver accepts (ACL passed) |
| `HotlinkReject` | 11 | `sid`, `rsn` | Receiver rejects |
| `HotlinkData` | 12 | `sid`, `seq`, `pth`, `etg`, `pay` | Payload frame with sequence number |
| `HotlinkClose` | 13 | `sid`, `rsn` | Close session |
| `HotlinkSignal` | 14 | `sid`, `knd`, `adr`, `tok`, `err` | WebRTC signaling (SDP + ICE) |

All messages are msgpack-encoded over WebSocket. WebRTC data uses a binary frame format (see IPC Framing below).

### Signal Kinds (HotlinkSignal type 14)

| kind | Purpose | `tok` field carries |
|------|---------|-------------------|
| `sdp_offer` | SDP offer | JSON `{type, sdp}` |
| `sdp_answer` | SDP answer | JSON `{type, sdp}` |
| `ice_candidate` | Trickle ICE | JSON RTCIceCandidateInit |
| `webrtc_error` | Error | error in `err` field |

No server changes needed — the server's `handleHotlinkSignal` is a pure relay that forwards any `kind` value. The existing fields (`sid`, `knd`, `tok`, `err`) carry everything.

### TCP Proxy Discovery

1. SyftBox client polls for `stream.tcp` marker files under datasites (Rust: 250ms, Go: 250ms)
2. Marker JSON: `{"from": "alice@...", "to": "bob@...", "port": 10001, "ports": {"alice@...": 10001, "bob@...": 10003}}`
3. Client computes canonical channel key from path PIDs (e.g., `alice@.../path/1_to_2/stream.tcp.request`)
4. Binds a TCP listener on `127.0.0.1:{port}` and stores the write half in `tcp_writers` map
5. When Sequre connects, client reads TCP data → sends as `HotlinkData` frames

### WebRTC Negotiation (Rust — completed)

**Offer side (receiver of HotlinkAccept):**
1. On `HotlinkAccept`, calls `handle_sdp_offer()` path:
   - Creates `WebRTCSession` via `create_webrtc_session()` (configures ICE servers, detaches data channels)
   - Creates ordered data channel (`syftbox-data`)
   - Sets `on_ice_candidate` callback → sends `HotlinkSignal(kind="ice_candidate")` via WS
   - Creates SDP offer, sets local description
   - Sends `HotlinkSignal(kind="sdp_offer", tok=json_sdp)` via WS server
   - Waits up to 10s for `ready_flag` (data channel open)

**Answer side (receives SDP offer):**
1. Receives `HotlinkSignal(kind="sdp_offer")`:
   - Creates `WebRTCSession` via `create_webrtc_session()`
   - Sets `on_ice_candidate` and `on_data_channel` callbacks
   - Sets remote description from offer SDP
   - Flushes any buffered ICE candidates
   - Creates SDP answer, sets local description
   - Sends `HotlinkSignal(kind="sdp_answer", tok=json_sdp)` via WS

**ICE candidate trickle:**
- **Send:** `on_ice_candidate` callback → `HotlinkSignal(kind="ice_candidate", tok=json)`
- **Receive:** `handle_ice_candidate()` → buffer if `remote_desc_set=false`, else `peer_connection.add_ice_candidate()`
- **Flush:** After `set_remote_description()`, drain `pending_candidates`

**Data channel:**
- Config: `ordered=true`, detached mode for raw `read()`/`write()` access
- Each WebRTC message = one complete HLNK frame (message-oriented, no scanning needed)
- `try_send_webrtc()` writes frame bytes directly to detached data channel
- Read loop reads from detached data channel, parses HLNK frame, dispatches to TCP writer

### IPC Framing (WebRTC + UNIX Socket)

```
[4B magic "HLNK"] [1B version=1] [2B path_len] [2B etag_len] [4B payload_len] [8B seq]
[path bytes] [etag bytes] [payload bytes]
```

### Reorder Buffer (Both Clients)

The Go server uses `runtime.NumCPU()` concurrent workers reading from a shared channel. Messages within a session can be relayed out of order. Both clients buffer incoming frames per channel:

**Rust:** `BTreeMap<u64, Vec<u8>>` keyed by seq, flushes consecutive frames from `next_seq`.

**Go:** `map[uint64][]byte` with the same logic — collect under `tcpMu` lock, write outside lock to avoid holding the lock during TCP IO.

### Telemetry (Rust Client Only)

Written to `{datasite}/.syftbox/hotlink_telemetry.json` every 1000ms:
- tx/rx packets and bytes
- P2P vs WS packet split (`tx_p2p_packets` > 0 means WebRTC is working)
- Send/write latency (avg, max)
- WebRTC offer/answer/fallback counters

### Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `SYFTBOX_HOTLINK` | `0` | Enable hotlink mode (WebRTC P2P + WS fallback + TCP proxy — all on) |
| `SYFTBOX_HOTLINK_ICE_SERVERS` | `stun:stun.l.google.com:19302` | Custom ICE (STUN/TURN) servers, comma-separated |
| `SYFTBOX_HOTLINK_TURN_USER` | (empty) | TURN server username (only needed for TURN URLs) |
| `SYFTBOX_HOTLINK_TURN_PASS` | (empty) | TURN server password (only needed for TURN URLs) |
| `SYFTBOX_HOTLINK_DEBUG` | `0` | Verbose logging |
| `SYFTBOX_HOTLINK_TCP_PROXY` | `0` | Enable TCP proxy marker scanning (set by `SYFTBOX_HOTLINK=1`) |
| `SYFTBOX_ACL_STAGING_GRACE_MS` | `600000` | ACL staging grace window in ms (set to `0` for fast test environments) |

**Removed (Rust):** `SOCKET_ONLY`, `TCP_PROXY_ADDR`, `QUIC`, `QUIC_ONLY`, `P2P`, `P2P_ONLY`, `STUN_SERVER`, `IPC`. When hotlink is enabled, everything is on with sensible defaults.

**Go client** still reads old env vars and uses QUIC transport until ported to WebRTC (see Go Porting Guide below).

---

## What's Done

### Phase 1: Hotlink over WebSocket + Local IPC ✅
- All protocol messages implemented (Open/Accept/Reject/Data/Close/Signal)
- Server routing with ACL-based accept/reject and broadcast filtering
- UNIX socket IPC in both Go and Rust clients
- E2E hotlink-protocol benchmark test

### Phase 1.5: TCP Tunnel over Hotlink ✅
- TCP proxy discovery via `stream.tcp` marker files
- Per-channel TCP listener binding and port mapping
- Writer readiness retry (up to 30s) for race where data arrives before Sequre connects
- TCP proxy paths never fall through to UNIX socket IPC
- Reorder buffer in Rust client (BTreeMap-based, handles server worker concurrency)
- Hard fail on send error (close TCP socket, no silent corruption)
- **Rust:** Fully working and tested
- **Go:** Fully working and tested (reorder buffer + buffer copy fix)

### Phase 2: QUIC P2P Transport ✅ (replaced by Phase 3)
- Was: QUIC via `quinn` (Rust) and `quic-go` (Go), worked on localhost/LAN
- Replaced by WebRTC for NAT traversal support
- QUIC code fully removed from Rust client

### Phase 3: WebRTC NAT Traversal ✅ (Rust)
- **Rust:** Complete. WebRTC data channels via `webrtc` crate v0.17 (webrtc-rs, tokio-based, pure Rust)
- Replaced ~700 lines of QUIC with ~310 lines of WebRTC
- Removed `quinn`, `rcgen`, `rustls` dependencies
- SDP offer/answer + ICE trickle over existing WS signaling
- TURN relay support via `SYFTBOX_HOTLINK_ICE_SERVERS` + `TURN_USER`/`TURN_PASS`
- Detached data channels for raw read/write (handles large MPC payloads)
- WS fallback when WebRTC unavailable
- No server changes needed — `handleHotlinkSignal` is a pure relay
- **Go:** Still uses QUIC, needs porting (see Go Porting Guide below)

### Docker NAT Traversal Test ✅
- Docker compose topology: `net-alice` (alice + server + minio + turn), `net-bob` (bob + server + turn)
- Alice and Bob on isolated networks, cannot ping each other
- TURN relay (coturn) bridges both networks
- Test proves WebRTC data channels open on BOTH sides through NAT via TURN
- CI workflow: `.github/workflows/nat-test.yml`
- Run locally: `bash docker/nat-test.sh` (or `NAT_TEST_CLEANUP=0 bash docker/nat-test.sh` to keep containers)

### Server ✅
- `handleHotlinkSignal` relay — forwards any signal kind (SDP, ICE, errors) between peers
- All 6 hotlink message types routed
- Session store with per-connection accepted tracking
- **No changes needed for WebRTC** — server is transport-agnostic

### Test Results (2026-02-07)
- **E2E TCP Proxy:** `TestHotlinkTCPProxy` passes both Rust and Go modes (20 chunks, 81920 bytes, correct order)
- **Docker NAT Test:** WebRTC data channels open on BOTH sides through NAT via TURN relay (~2s negotiation)
- **Distributed scenario:** All 3 Sequre parties completed, MPC result `[3,3,4]` correct

### Benchmark Results (hotlink-protocol, IPC round-trip)
| Metric | Go | Rust |
|--------|-----|------|
| P50 | ~330us | ~970us |
| P90 | ~870us | ~4.8ms |
| P95 | ~880us | ~5.5ms |
| P99 | ~1.1ms | ~5.8ms |

---

## What's Left

### Required for Production

1. **Deploy server** with `MsgHotlinkSignal` (type 14). Without it, WebRTC negotiation fails silently → WS-only fallback.

2. **Go client WebRTC port** — Replace QUIC with WebRTC using `pion/webrtc` v4 (see Go Porting Guide below).

3. **Aggregator telemetry** — Connections show "pending" despite scenario passing. Investigate if reporting bug or real issue.

4. **TURN server deployment** — Run coturn (or embedded `pion/turn`) alongside production server for symmetric NAT fallback.

### Nice to Have

- **Rust latency optimization** — Go is ~3-5x faster per-message. Opportunities: channel-based message passing, `parking_lot` mutexes, reduce tokio overhead.
- **Windows named pipe** (`stream.pipe`) — Not implemented.
- **TCP IPC for containers** — Where UNIX sockets don't work.
- **File fallback replay** — Write `.request` files on hotlink failure, replay into IPC.
- **OTEL tracing** — Per-stage latency spans.
- **ICE-only upgrade path** — `webrtc-ice` subcrate → hand raw UDP socket to `quinn` → QUIC over NAT-punched path. Only if SCTP proves to be a bottleneck.

---

## Go Client WebRTC Porting Guide

This section describes exactly what the Go client needs to change to replace QUIC with WebRTC, matching the Rust implementation.

### Library

Use **`pion/webrtc` v4** (`github.com/pion/webrtc/v4`). Pure Go, no CGo, tokio-equivalent goroutine model. webrtc-rs (used in Rust) is a port of pion, so the API is nearly identical.

### Dependencies to Remove

```go
// Remove from go.mod:
github.com/quic-go/quic-go
// Remove any rcgen/rustls equivalents (Go used crypto/tls with self-signed certs)
```

### Dependencies to Add

```go
// Add to go.mod:
github.com/pion/webrtc/v4
```

### Struct Changes

Replace `quicSession`/`quicOutbound` with:

```go
type WebRTCSession struct {
    PeerConnection   *webrtc.PeerConnection
    DataChannel      *webrtc.DataChannel
    Ready            chan struct{}       // closed when data channel opens
    ReadyFlag        atomic.Bool
    Err              error
    PendingCandidates []webrtc.ICECandidateInit
    RemoteDescSet    atomic.Bool
    mu               sync.Mutex         // protects PendingCandidates
}
```

### ICE Server Configuration

Read the same env vars as Rust:

```go
func iceServers() []webrtc.ICEServer {
    raw := os.Getenv("SYFTBOX_HOTLINK_ICE_SERVERS")
    if raw == "" { raw = os.Getenv("SYFTBOX_HOTLINK_STUN_SERVER") }
    if raw == "" { raw = "stun:stun.l.google.com:19302" }
    user := os.Getenv("SYFTBOX_HOTLINK_TURN_USER")
    pass := os.Getenv("SYFTBOX_HOTLINK_TURN_PASS")

    var servers []webrtc.ICEServer
    for _, url := range strings.Split(raw, ",") {
        url = strings.TrimSpace(url)
        s := webrtc.ICEServer{URLs: []string{url}}
        if strings.HasPrefix(url, "turn:") || strings.HasPrefix(url, "turns:") {
            s.Username = user
            s.Credential = pass
        }
        servers = append(servers, s)
    }
    return servers
}
```

### Function Replacement Map

| Remove (QUIC) | Add (WebRTC) | Notes |
|----------------|-------------|-------|
| `maybeStartQuicOffer()` | (trigger on HotlinkAccept) | Create PeerConnection, DataChannel, SDP offer, ICE callbacks |
| `acceptQuic()` | (removed) | DataChannel `OnOpen` replaces this |
| `handleQuicOffer()` | `handleSDPOffer()` | Set remote desc, create answer, flush pending ICE |
| `handleQuicAnswer()` | `handleSDPAnswer()` | Set remote desc, flush pending ICE |
| `trySendQuic()` | `trySendWebRTC()` | `dataChannel.Send(frameBytes)` |
| `quicReadLoop()` | (OnMessage callback or Detach) | WebRTC data channel is callback/detach-based |
| QUIC setup (certs, listener, dialer) | `createWebRTCSession()` (~30 lines) | WebRTC handles TLS/DTLS internally |
| (new) | `handleICECandidate()` | Buffer or apply ICE candidates |

### Signal Dispatch

Update the signal handler to match new kinds:

```go
func (hm *HotlinkManager) handleSignal(signal *HotlinkSignal) {
    switch signal.Kind {
    case "sdp_offer":
        hm.handleSDPOffer(signal)
    case "sdp_answer":
        hm.handleSDPAnswer(signal)
    case "ice_candidate":
        hm.handleICECandidate(signal)
    case "webrtc_error":
        log.Printf("webrtc error from peer: %s", signal.Err)
    }
}
```

### Creating a WebRTC Session

```go
func createWebRTCSession() (*WebRTCSession, error) {
    config := webrtc.Configuration{
        ICEServers: iceServers(),
    }
    se := webrtc.SettingEngine{}
    se.DetachDataChannels()  // for raw read/write access

    api := webrtc.NewAPI(
        webrtc.WithSettingEngine(se),
    )

    pc, err := api.NewPeerConnection(config)
    if err != nil {
        return nil, err
    }

    return &WebRTCSession{
        PeerConnection: pc,
        Ready:          make(chan struct{}),
    }, nil
}
```

### SDP Offer (called after HotlinkAccept)

```go
func (hm *HotlinkManager) startWebRTCOffer(sessionID string) {
    sess, err := createWebRTCSession()
    // ... store sess in outbound map

    // Create data channel
    dc, err := sess.PeerConnection.CreateDataChannel("syftbox-data", &webrtc.DataChannelInit{
        Ordered: boolPtr(true),
    })
    sess.DataChannel = dc

    // ICE candidate callback
    sess.PeerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
        if c == nil { return }
        candidateJSON, _ := json.Marshal(c.ToJSON())
        hm.sendSignal(sessionID, "ice_candidate", string(candidateJSON), "")
    })

    // Data channel open callback
    dc.OnOpen(func() {
        sess.ReadyFlag.Store(true)
        close(sess.Ready)
    })

    // Data channel message callback (or use Detach for raw bytes)
    dc.OnMessage(func(msg webrtc.DataChannelMessage) {
        hm.handleWebRTCData(sessionID, msg.Data)
    })

    // Create and send offer
    offer, err := sess.PeerConnection.CreateOffer(nil)
    sess.PeerConnection.SetLocalDescription(offer)
    offerJSON, _ := json.Marshal(offer)
    hm.sendSignal(sessionID, "sdp_offer", string(offerJSON), "")
}
```

### SDP Answer (handle incoming offer)

```go
func (hm *HotlinkManager) handleSDPOffer(signal *HotlinkSignal) {
    sess, _ := createWebRTCSession()
    // ... store sess in sessions map

    // ICE candidate callback (same as offer side)
    sess.PeerConnection.OnICECandidate(func(c *webrtc.ICECandidate) { ... })

    // Data channel callback (answer side receives DC from offer side)
    sess.PeerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
        sess.DataChannel = dc
        dc.OnOpen(func() {
            sess.ReadyFlag.Store(true)
            close(sess.Ready)
        })
        dc.OnMessage(func(msg webrtc.DataChannelMessage) {
            hm.handleWebRTCData(signal.SessionID, msg.Data)
        })
    })

    // Set remote description (the offer)
    var offer webrtc.SessionDescription
    json.Unmarshal([]byte(signal.Token), &offer)
    sess.PeerConnection.SetRemoteDescription(offer)
    sess.RemoteDescSet.Store(true)

    // Flush buffered ICE candidates
    sess.mu.Lock()
    for _, c := range sess.PendingCandidates {
        sess.PeerConnection.AddICECandidate(c)
    }
    sess.PendingCandidates = nil
    sess.mu.Unlock()

    // Create and send answer
    answer, _ := sess.PeerConnection.CreateAnswer(nil)
    sess.PeerConnection.SetLocalDescription(answer)
    answerJSON, _ := json.Marshal(answer)
    hm.sendSignal(signal.SessionID, "sdp_answer", string(answerJSON), "")
}
```

### ICE Candidate Handling

```go
func (hm *HotlinkManager) handleICECandidate(signal *HotlinkSignal) {
    var candidate webrtc.ICECandidateInit
    json.Unmarshal([]byte(signal.Token), &candidate)

    sess := hm.getSession(signal.SessionID)
    if sess == nil { return }

    if sess.RemoteDescSet.Load() {
        sess.PeerConnection.AddICECandidate(candidate)
    } else {
        // Buffer until remote description is set
        sess.mu.Lock()
        sess.PendingCandidates = append(sess.PendingCandidates, candidate)
        sess.mu.Unlock()
    }
}
```

### Sending Data via WebRTC

```go
func (hm *HotlinkManager) trySendWebRTC(sessionID string, frame []byte) (bool, error) {
    sess := hm.getSession(sessionID)
    if sess == nil || !sess.ReadyFlag.Load() {
        return false, nil  // not ready → WS fallback
    }
    err := sess.DataChannel.Send(frame)
    if err != nil {
        return false, err  // error → WS fallback
    }
    return true, nil  // sent via P2P
}
```

### Key Differences from QUIC

| Aspect | QUIC (old) | WebRTC (new) |
|--------|-----------|-------------|
| NAT traversal | Manual STUN + direct dial | Automatic ICE (STUN + TURN) |
| TLS/certs | Self-signed, manual | Built-in DTLS, automatic |
| Connection | Direct UDP dial to address list | ICE negotiates best path |
| Signaling | `quic_offer`/`quic_answer` with addrs | `sdp_offer`/`sdp_answer`/`ice_candidate` |
| Symmetric NAT | Fails (needs port prediction) | Works via TURN relay |
| Setup lines | ~324 lines (Rust) | ~30 lines (Rust) |
| Dependencies | quinn + rcgen + rustls | single `webrtc` crate |
| Read model | Stream-based `quic_read_loop` | Message-oriented callbacks/detach |

### Testing

1. **Unit:** `cargo test` / `go test` — basic compilation and struct tests
2. **E2E (local):** `just sbdev-test-single TestHotlinkTCPProxy mode=rust` (or `mode=go`)
3. **NAT traversal (Docker):** `bash docker/nat-test.sh` — isolated networks, TURN relay, proves P2P through NAT
4. **Telemetry check:** After E2E test, verify `tx_p2p_packets > 0` in `hotlink_telemetry.json`

### Docker NAT Test Environment

For testing NAT traversal locally without real NAT:

```bash
# Run the test (builds containers, runs test, cleans up)
bash docker/nat-test.sh

# Keep containers running for debugging
NAT_TEST_CLEANUP=0 bash docker/nat-test.sh

# Check logs
docker logs nat-alice 2>&1 | grep -i "webrtc\|ice\|data.channel"
docker logs nat-bob 2>&1 | grep -i "webrtc\|ice\|data.channel"
docker logs nat-turn 2>&1 | tail -20
```

**Docker compose topology** (`docker/docker-compose-nat-test.yml`):
- `net-alice`: alice + server + minio + turn
- `net-bob`: bob + server + turn
- Alice and Bob are on DIFFERENT networks (cannot ping each other)
- Server + TURN bridge both networks
- WebRTC must use TURN relay to establish data channels

**Key env vars for Docker test:**
```yaml
SYFTBOX_HOTLINK: 1
SYFTBOX_HOTLINK_ICE_SERVERS: turn:turn:3478?transport=udp
SYFTBOX_HOTLINK_TURN_USER: syftbox
SYFTBOX_HOTLINK_TURN_PASS: syftbox
SYFTBOX_HOTLINK_TCP_PROXY: 1
SYFTBOX_HOTLINK_DEBUG: 1
SYFTBOX_PRIORITY_DEBOUNCE_MS: 0
SYFTBOX_ACL_STAGING_GRACE_MS: 0
```

---

## Lessons Learned / Pitfalls

These are bugs we hit during development. Keeping them here so we don't repeat them.

### 1. Tokio `notify_waiters()` Does Not Buffer (Rust)
**Symptom:** All hotlink connections stuck "pending" forever on localhost.
**Cause:** `Notify::notify_waiters()` only wakes tasks that are *already* polling `notified()`. On localhost, `HotlinkAccept` round-trips so fast it arrives in the gap between dropping the read lock (after checking `accepted=false`) and entering `notified().await`. The notification is lost.
**Fix:** Use `notify_one()` which buffers a single permit. The permit is consumed when `notified()` is later polled.
**Rule:** Never use `notify_waiters()` for point-to-point wake-ups where the waiter may not be polling yet.

### 2. Server Concurrent Workers Reorder Messages
**Symptom:** Sequre segfault (signal 11) — TCP stream corruption from out-of-order writes.
**Cause:** Go server uses `runtime.NumCPU()` goroutines reading from a shared channel. `HotlinkData` messages within the same session are processed by different workers and relayed in arbitrary order. TCP is a byte stream — writing chunks out of order corrupts it.
**Fix:** Client-side reorder buffer (`BTreeMap<seq, data>`) that buffers and flushes in sequence order.
**Rule:** Never assume message ordering through the server when concurrent workers exist. Always use sequence numbers and reorder on the receiving end.

### 3. TCP Writer Not Ready When First Frame Arrives
**Symptom:** `hotlink tcp write skipped: no writer for path=...` → deadlock (MPC peers wait forever).
**Cause:** Sequre hasn't connected to the local TCP port yet when the first `HotlinkData` frame arrives from the remote peer.
**Fix:** `getTCPWriterWithRetry` loops up to 30s (60 retries × 500ms) waiting for the writer to appear.
**Rule:** The receiving TCP proxy must tolerate the writer not being ready immediately. Never drop frames silently — either buffer/retry or fail hard.

### 4. TCP Proxy Paths Falling Through to IPC
**Symptom:** After TCP write skip, code tried UNIX socket IPC for a TCP proxy path → `ipc accept timeout`.
**Cause:** `handle_frame` didn't return early for TCP proxy paths when the writer was missing, falling through to the IPC socket code path.
**Fix:** Explicit early return for `is_tcp_proxy_path()` frames — never attempt IPC socket fallback for TCP proxy sessions.
**Rule:** TCP proxy and IPC socket are separate code paths. Never mix them.

### 5. Codon JIT SIGSEGV from Pointer-Heavy Code (Sequre/Syqure)
**Symptom:** `SIGSEGV` at "MHE generating relinearization key" — unrelated to hotlink data flow.
**Cause:** Adding 233+ lines of pointer-heavy hotlink IPC code (raw `ptr[byte]`, `sockaddr_in`, C FFI) into `file_transport.codon` corrupts Codon's LLVM JIT codegen for MHE lattice operations. The JIT compiles all reachable functions at load time.
**Fix:** Split into separate `hotlink_transport.codon` module with lazy conditional imports. In TCP proxy mode, `run_dynamic.rs` sets `SEQURE_TRANSPORT=tcp` so the hotlink IPC code is never compiled.
**Rule:** Keep pointer-heavy FFI code in separate Codon modules. The JIT can corrupt codegen for unrelated functions when complex C-interop code is compiled in the same compilation unit.

### 6. RwLock Deadlock Pattern (Rust)
**Symptom:** Client hangs forever during `send_hotlink`.
**Cause:** `if let Some(x) = map.read().await.get(&k) { ... } else { map.write().await... }` — the read guard isn't dropped before the write lock is requested.
**Fix:** Scope the read guard explicitly:
```rust
let existing = { let g = map.read().await; g.get(&k).cloned() };
if let Some(x) = existing { ... } else { map.write().await... }
```
**Rule:** Always explicitly scope `RwLock` read guards in Rust async code before attempting write acquisition.

### 7. UNIX Socket Listener Recreation Race (Rust)
**Symptom:** Intermittent connection failures on IPC socket.
**Cause:** Listener was recreated every loop iteration after accept timeout, removing the socket file while a client was mid-connect.
**Fix:** Create listener once at startup, reuse for all connections.

### 8. OutputCapture fork() SIGABRT (Syqure)
**Symptom:** `SIGABRT` when Syqure runner captures output.
**Cause:** `OutputCapture` in bridge.cc calls `fork()`, which is unsafe in multi-threaded Rust tokio runtime.
**Fix:** Disabled `OutputCapture` in bridge.cc.

### 9. Sync Conflict for Progress Files
**Symptom:** `state.json` renamed to `state.conflict.json`, breaking flow coordination between parties.
**Cause:** Multiple parties write to `_progress/state.json` concurrently. The sync engine detects a conflict and renames the file.
**Fix:** Added "local wins" semantics for `_progress/` directories in `rust/src/sync.rs`. When a conflict is detected on a progress path, the local version is uploaded instead of creating a conflict file.
**Rule:** Flow progress files should always use local-wins conflict resolution since each party owns its own progress.

### 10. TCP Proxy Buffer Reuse Corruption (Go)
**Symptom:** `chunk 0: expected index 0, got 2 (out of order)` — data arrives in reorder-buffer sequence order but payload content is wrong.
**Cause:** `runTCPProxy` reuses a single 64KB `[]byte` buffer for all TCP reads. `Events.Send` is async — it queues the `*Message` (containing a slice reference to the buffer) into a channel. The goroutine loops back and calls `Read()` into the same buffer before the previous message is serialized over WebSocket, corrupting the payload.
**Fix:** Copy the payload before passing to `sendHotlink`: `payload := make([]byte, n); copy(payload, buf[:n])`.
**Rule:** Never pass a reusable buffer slice to an async send. Always copy if the buffer will be reused before the consumer reads it.

### 11. Workspace Lock Stale Processes
**Symptom:** `workspace locked by another process` errors on test startup.
**Cause:** Previous test run left stale `syftbox`, `bv syftboxd`, or `devstack` processes holding workspace locks.
**Fix:** Kill all related processes and remove `sandbox/` directory and `*.lock` files before re-running.
**Rule:** Always clean up processes before running scenarios: `pkill -f syftbox; pkill -f devstack; pkill -f sequre`

### 12. pipefail + grep -q SIGPIPE in Bash (Docker NAT test)
**Symptom:** `set -euo pipefail` script exits unexpectedly when `docker logs | grep -q "pattern"` matches.
**Cause:** `grep -q` exits immediately on first match. The upstream `docker logs` command receives SIGPIPE (exit 141). With `pipefail`, the pipeline returns non-zero even though grep found the match.
**Fix:** Capture output to a variable first, then use bash pattern matching: `has() { [[ "$1" == *"$2"* ]]; }` — no piping at all.
**Rule:** Never pipe to `grep -q` in `pipefail` scripts. Use variable capture + `[[ ]]` pattern matching instead.

### 13. ACL Staging Grace Window Blocks Test Sync
**Symptom:** Docker NAT test times out — files never sync between peers.
**Cause:** ACL staging grace window defaults to 600s (10 minutes). New ACL files are quarantined for this period before being applied, blocking all file sync that depends on them.
**Fix:** Set `SYFTBOX_ACL_STAGING_GRACE_MS=0` in test environments.
**Rule:** Always set `SYFTBOX_ACL_STAGING_GRACE_MS=0` in test docker-compose files and CI environments.

---

## Files Changed (Branch: `madhava/hotlink`)

| File | Delta | What |
|------|-------|------|
| `rust/src/hotlink_manager.rs` | +2176 | WebRTC P2P, TCP proxy, reorder buffer, telemetry, ICE/TURN |
| `internal/client/sync/hotlink_manager.go` | +1050 | QUIC (to be replaced), TCP proxy, STUN, reorder buffer |
| `cmd/devstack/hotlink_tcp_proxy_test.go` | +223 | E2E integration test for TCP proxy (Rust + Go) |
| `internal/server/server.go` | +33 | `handleHotlinkSignal` relay |
| `internal/syftmsg/msg_hotlink.go` | +22 | `HotlinkSignal` struct |
| `internal/syftmsg/msg_type.go` | +3 | `MsgHotlinkSignal = 14` |
| `internal/syftmsg/msg.go` | +6 | Unmarshal for HotlinkSignal |
| `internal/wsproto/codec.go` | +15 | Msgpack for HotlinkSignal |
| `rust/src/wsproto.rs` | +58 | HotlinkSignal encode/decode |
| `rust/Cargo.toml` | +1 | webrtc v0.17 (removed quinn/rcgen/rustls) |
| `rust/src/client.rs` | +4 | WS dispatch for HotlinkSignal |
| `rust/src/hotlink.rs` | +20 | `parse_hotlink_frame_from_bytes()` + minor |
| `rust/src/sync.rs` | +18 | TCP proxy wiring |
| `docker/docker-compose-nat-test.yml` | +138 | NAT simulation topology |
| `docker/Dockerfile.client.rust` | +39 | Rust client container |
| `docker/nat-test.sh` | +222 | NAT traversal test script |
| `docker/entrypoint-nat-test.sh` | +30 | Client entrypoint for NAT test |
| `.github/workflows/nat-test.yml` | +57 | CI workflow for NAT test |

## Running

```bash
# Kill leftover processes
pkill -f syftbox; pkill -f devstack; pkill -f sequre; sleep 2

# E2E test (Rust client, local)
just sbdev-test-single TestHotlinkTCPProxy mode=rust

# E2E test (Go client, local)
just sbdev-test-single TestHotlinkTCPProxy mode=go

# Docker NAT traversal test
bash docker/nat-test.sh

# Docker NAT test (keep containers for debugging)
NAT_TEST_CLEANUP=0 bash docker/nat-test.sh

# Full distributed scenario (from biovault dir)
cd biovault && go run ./cmd/devstack scenario run syqure-distributed.yaml

# Benchmark
./benchmark.sh --bench=hotlink-protocol --lang=rust
./benchmark.sh --bench=hotlink-protocol --lang=go
```
