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
  |                              |===== 7. QUIC offer/answer (HotlinkSignal via server) =====>|                        |
  |                              |<================ 8. QUIC connection established ==========>|                        |
  |                              |                              |                              |                        |
  |  9. TCP send(data) -------->|                              |                              |                        |
  |                              | 10. HotlinkData(seq=N) via QUIC =========================>|                        |
  |                              |                              |                              | 11. reorder → TCP ---->|
  |                              |                              |                              |                        | 12. recv(data)
```

If QUIC fails or is disabled, steps 7-8 are skipped and step 10 goes via WebSocket through the server.

**Phase 3 target:** Replace QUIC (steps 7-8) with WebRTC data channels for automatic NAT traversal:
```
  |  7. SDP offer/answer + ICE candidates (via HotlinkSignal over WS)     |
  |  8. WebRTC data channel established (P2P through NAT, or TURN relay)  |
```

### Protocol Messages

| Type | ID | Fields | Purpose |
|------|----|--------|---------|
| `HotlinkOpen` | 9 | `sid`, `pth` | Sender opens session for a path |
| `HotlinkAccept` | 10 | `sid` | Receiver accepts (ACL passed) |
| `HotlinkReject` | 11 | `sid`, `rsn` | Receiver rejects |
| `HotlinkData` | 12 | `sid`, `seq`, `pth`, `etg`, `pay` | Payload frame with sequence number |
| `HotlinkClose` | 13 | `sid`, `rsn` | Close session |
| `HotlinkSignal` | 14 | `sid`, `knd`, `adr`, `tok`, `err` | QUIC signaling (offer/answer) |

All messages are msgpack-encoded over WebSocket. QUIC data uses a binary frame format (see IPC Framing below).

### TCP Proxy Discovery

1. SyftBox client polls for `stream.tcp` marker files under datasites (Rust: 250ms, Go: 250ms)
2. Marker JSON: `{"from": "alice@...", "to": "bob@...", "port": 10001, "ports": {"alice@...": 10001, "bob@...": 10003}}`
3. Client computes canonical channel key from path PIDs (e.g., `alice@.../path/1_to_2/stream.tcp.request`)
4. Binds a TCP listener on `127.0.0.1:{port}` and stores the write half in `tcp_writers` map
5. When Sequre connects, client reads TCP data → sends as `HotlinkData` frames

### QUIC Negotiation

**Receiver (offer side):**
1. On `HotlinkAccept`, calls `maybe_start_quic_offer()`
2. Generates self-signed TLS cert, binds UDP on random port, starts QUIC listener (ALPN: `syftbox-hotlink`)
3. Optionally runs STUN binding request to discover public IP:port
4. Sends `HotlinkSignal(kind="quic_offer", addrs=[local, stun])` via server
5. Waits 2500ms for incoming QUIC connection
6. On connect: reads handshake (`HLQ1` + session_id), stores stream, sets ready flag
7. Spawns `quic_read_loop()` to receive frames on the QUIC stream

**Sender (answer side):**
1. Receives offer, tries each address with 1500ms dial timeout
2. On connect: writes `HLQ1{len}{session_id}` handshake
3. Sends `HotlinkSignal(kind="quic_answer", token="ok")` or `error` on failure
4. If all addresses fail and `QUIC_ONLY=0`, falls back to WS

### IPC Framing (QUIC + UNIX Socket)

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
- QUIC vs WS packet split
- Send/write latency (avg, max)
- QUIC offer/answer/fallback counters

### Environment Variables (Simplified)

| Variable | Default | Purpose |
|----------|---------|---------|
| `SYFTBOX_HOTLINK` | `0` | Enable hotlink mode (WebRTC P2P + WS fallback + TCP proxy — all on) |
| `SYFTBOX_HOTLINK_ICE_SERVERS` | `stun:stun.l.google.com:19302` | Custom ICE (STUN/TURN) servers, comma-separated |
| `SYFTBOX_HOTLINK_DEBUG` | `0` | Verbose logging |

**Removed (Rust):** `SOCKET_ONLY`, `TCP_PROXY`, `TCP_PROXY_ADDR`, `QUIC`, `QUIC_ONLY`, `P2P`, `P2P_ONLY`, `STUN_SERVER`, `IPC`. When hotlink is enabled, everything is on with sensible defaults. No knobs needed.

**Go client** still reads old env vars for backwards compatibility until ported to WebRTC.

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

### Phase 2: QUIC P2P Transport ✅ (localhost/LAN only)
- QUIC via `quinn` (Rust) and `quic-go` (Go)
- WS signaling for offer/answer via `MsgHotlinkSignal` relayed by server
- Self-signed TLS certs per session
- Mixed transport (some peers QUIC, others WS)
- QUIC-only mode for testing
- Initial STUN candidate discovery in both clients
- Telemetry in Rust client (tx/rx, quic/ws split, latency)

### Server ✅
- `handleHotlinkSignal` relay for QUIC negotiation
- All 6 hotlink message types routed
- Session store with per-connection accepted tracking

### Test Results (2026-02-07)
- **Scenario:** `syqure-distributed.yaml` with `BV_DEVSTACK_CLIENT_MODE=rust`
- **Result:** All 3 Sequre parties completed, MPC result `[3,3,4]` correct
- **Duration:** Aggregator ~40s, clients ~45s
- **Transport:** QUIC preferred (q17198/ws26, q17101/ws51)
- **Reproducible:** Passed on consecutive runs

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

1. **Deploy server** with `MsgHotlinkSignal` (type 14). Without it, QUIC negotiation fails silently → WS-only fallback.

2. ~~**Go client reorder buffer**~~ ✅ Done — ported `tcpReorderBuf` to Go, plus buffer copy fix for async WS send.

3. ~~**Integration test in syftbox repo**~~ ✅ Done — `cmd/devstack/hotlink_tcp_proxy_test.go` tests TCP proxy data flow with reorder verification for both Rust and Go clients. Both pass (20 chunks, 81920 bytes, correct order).

4. **Aggregator telemetry** — Connections show "pending" despite scenario passing. Investigate if reporting bug or real issue.

### Phase 3: WebRTC NAT Traversal (In Progress)

Replace the custom QUIC+STUN negotiation with WebRTC data channels for automatic NAT traversal. The syftbox server already acts as a signaling server (both clients have open WS connections). WebRTC's ICE handles ~92-95% of NATs automatically; TURN relay covers the rest.

**Libraries:**
- Rust: `webrtc` crate v0.17 (webrtc-rs, tokio-based, pure Rust port of pion)
- Go: `pion/webrtc` v4 (pure Go, no CGo) — later phase

**Connection upgrade strategy:**
```
1. Direct data channel (P2P, ~60% of NATs)       ← fastest
2. STUN hole punch via ICE (additional ~30%)      ← still P2P
3. TURN relay on server (symmetric NAT, ~5-8%)    ← server-relayed but lighter than WS
4. WS relay (current fallback, always works)      ← heaviest overhead
```

**Future upgrade path:** ICE-only path discovery (`webrtc-ice` subcrate) → hand raw UDP socket to `quinn` → run QUIC streams over NAT-punched path. Only if benchmarks show SCTP is a bottleneck.

**Signaling flow (over existing WS):**
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

**Signal kinds (reuse HotlinkSignal type 14):**

| kind | Purpose | `tok` field carries |
|------|---------|-------------------|
| `sdp_offer` | SDP offer | JSON `{type, sdp}` |
| `sdp_answer` | SDP answer | JSON `{type, sdp}` |
| `ice_candidate` | Trickle ICE | JSON RTCIceCandidateInit |
| `webrtc_error` | Error | error in `err` field |

No wsproto/server changes needed — existing fields (`sid`, `knd`, `tok`, `err`) carry everything. Server's `handleHotlinkSignal` is a pure relay that forwards any `kind` value.

**Rust implementation (files to modify):**

| File | Change |
|------|--------|
| `rust/Cargo.toml` | Remove quinn/rcgen/rustls, add webrtc v0.17 |
| `rust/src/hotlink_manager.rs` | Replace ~700 lines of QUIC with ~310 lines of WebRTC |
| `rust/src/hotlink.rs` | Add `parse_hotlink_frame_from_bytes()` for message-oriented parsing |

**Function replacement map:**

| Remove (QUIC) | Add (WebRTC) | Notes |
|----------------|-------------|-------|
| `maybe_start_quic_offer()` | `start_webrtc_offer()` | Create PeerConnection + DataChannel, SDP offer, ICE callbacks |
| `accept_quic()` | (removed) | DataChannel `on_open` replaces this |
| `handle_quic_offer()` | `handle_sdp_offer()` | Set remote desc, create answer |
| `handle_quic_answer()` | `handle_sdp_answer()` | Set remote desc, flush pending ICE |
| `try_send_quic()` | `try_send_webrtc()` | `data_channel.send(&Bytes::from(frame))` |
| `quic_read_loop()` | (on_message callback) | WebRTC data channel is callback-based |
| QUIC setup (~324 lines) | `create_webrtc_api()` (~30 lines) | WebRTC handles TLS/certs/STUN internally |
| (new) | `handle_ice_candidate()` | Buffer or apply ICE candidates |

**WebRTC session struct:**
```rust
struct WebRTCSession {
    peer_connection: Arc<RTCPeerConnection>,
    data_channel: Arc<RTCDataChannel>,
    ready: Arc<Notify>,
    ready_flag: Arc<AtomicBool>,
    err: Arc<TokioMutex<Option<String>>>,
    pending_candidates: Arc<TokioMutex<Vec<RTCIceCandidateInit>>>,
}
```

**Data channel config:** ordered=true, no retransmit limit. Use `on_message` callback for receiving (each WebRTC message = one complete HLNK frame, message-oriented).

**ICE candidate trickle:**
- **Send:** `on_ice_candidate` callback → `HotlinkSignal(kind="ice_candidate", tok=json)`
- **Receive:** `handle_ice_candidate()` → `peer_connection.add_ice_candidate()` or buffer if remote desc not yet set
- **Flush:** After `set_remote_description()`, drain `pending_candidates`

**WS fallback (same pattern as current QUIC):**
```
try_send_webrtc() → Ok(Some(())) → done
                  → Ok(None)     → WS fallback
                  → Err(_)       → WS fallback (unless p2p_only)
```

**Env variable simplification (Rust):**

All granular env vars removed. `SYFTBOX_HOTLINK=1` enables everything:
- WebRTC P2P (with ICE NAT traversal)
- WS fallback (always available)
- TCP proxy (for MPC channels)
- IPC socket discovery

Only optional overrides: `SYFTBOX_HOTLINK_ICE_SERVERS` (custom STUN/TURN) and `SYFTBOX_HOTLINK_DEBUG` (verbose logging).

**Implementation steps:**

5. **Cargo.toml** — Remove quinn/rcgen/rustls, add `webrtc = "0.17"`. Verify `cargo build`.

6. **Frame parser** — Add `parse_hotlink_frame_from_bytes()` to `hotlink.rs`. WebRTC data channels are message-oriented (each message = one complete HLNK frame), so no magic-byte scanning needed.

7. **WebRTC transport in Rust** — Replace all QUIC code in `hotlink_manager.rs`:
   - `WebRTCSession` struct replaces `HotlinkQuicSession`/`HotlinkQuicOutbound`
   - `create_webrtc_api()` replaces QUIC server/client config (~30 lines vs ~324)
   - SDP offer/answer via `HotlinkSignal` (kind=`sdp_offer`/`sdp_answer`)
   - Trickle ICE via `HotlinkSignal` (kind=`ice_candidate`)
   - `on_message` callback replaces `quic_read_loop`
   - Remove: `AcceptAnyCert`, STUN discovery, QUIC handshake, all `quinn` imports

8. **Docker NAT simulation test** — Two isolated Docker networks (alice can't reach bob), server bridges both.
   - `docker-compose.yml` with `net-alice`, `net-bob`, server on both
   - Integration test that sends TCP proxy data and verifies delivery

9. **TURN relay on server** — Run a TURN server alongside the syftbox server for symmetric NAT fallback.
   - Use `pion/turn` (Go) — lightweight, embeddable TURN server
   - Server provides TURN credentials to clients via `HotlinkSignal`

10. **Go client WebRTC** — Port Rust implementation to Go using `pion/webrtc` v4.

11. **Remove custom QUIC** — Once WebRTC data channels handle all transport in both clients, remove `quinn`/`quic-go` dependencies and custom STUN code.

### Nice to Have

9. **Rust latency optimization** — Go is ~3-5x faster per-message. Opportunities: channel-based message passing, `parking_lot` mutexes, reduce tokio overhead.

10. **Windows named pipe** (`stream.pipe`) — Not implemented.

11. **TCP IPC for containers** — Where UNIX sockets don't work.

12. **File fallback replay** — Write `.request` files on hotlink failure, replay into IPC.

13. **OTEL tracing** — Per-stage latency spans.

14. **Syqure bundle rebuild** — Rebuild tarball with updated sequre stdlib.

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

---

## Files Changed (Branch: `madhava/hotlink`)

| File | Delta | What |
|------|-------|------|
| `rust/src/hotlink_manager.rs` | +1680 | QUIC, TCP proxy, reorder buffer, telemetry, STUN, notify_one fix |
| `internal/client/sync/hotlink_manager.go` | +1050 | QUIC, TCP proxy, STUN, reorder buffer, buffer copy fix |
| `cmd/devstack/hotlink_tcp_proxy_test.go` | +223 | E2E integration test for TCP proxy (Rust + Go) |
| `internal/server/server.go` | +33 | `handleHotlinkSignal` relay |
| `internal/syftmsg/msg_hotlink.go` | +22 | `HotlinkSignal` struct |
| `internal/syftmsg/msg_type.go` | +3 | `MsgHotlinkSignal = 14` |
| `internal/syftmsg/msg.go` | +6 | Unmarshal for HotlinkSignal |
| `internal/wsproto/codec.go` | +15 | Msgpack for HotlinkSignal |
| `rust/src/wsproto.rs` | +58 | HotlinkSignal encode/decode |
| `rust/Cargo.toml` | +3 | quinn, rcgen, rustls |
| `rust/src/client.rs` | +4 | WS dispatch for HotlinkSignal |
| `rust/src/hotlink.rs` | +1 | Minor |
| `rust/src/sync.rs` | +18 | TCP proxy wiring |
| `internal/client/sync/sync_engine.go` | +3 | Wiring |
| `internal/client/sync/acl_staging.go` | +1 | ACL grace window |

## Running

```bash
# Kill leftover processes
pkill -f syftbox; pkill -f devstack; pkill -f sequre; sleep 2

# Run distributed scenario (from biovault dir)
cd biovault && go run ./cmd/devstack scenario run syqure-distributed.yaml

# Run hotlink-protocol benchmark
./benchmark.sh --bench=hotlink-protocol --lang=rust
./benchmark.sh --bench=hotlink-protocol --lang=go
```
