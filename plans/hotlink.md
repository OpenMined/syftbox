# Hotlink Plan

## Goals
- Provide the **lowest-latency** data path between peers for time-critical MPC/HE workloads.
- Keep existing file-based durability as an optional fallback (do not slow the primary path).
- Keep the **application interface stable** (e.g., always read from a FIFO), even if hotlink drops.
- Support both Go and Rust clients; enable devstack E2E latency benchmarking.
- Enable a **low-latency TCP tunnel** mode for Syqure-style socket streams.

## Architecture

### How Hotlink Works (End-to-End)

```
Sequre (CP1)                    SyftBox Client (Rust)              SyftBox Server (Go)            SyftBox Client (Rust)              Sequre (CP2)
    |                                |                                  |                                |                              |
    |-- TCP connect localhost:10001 ->|                                  |                                |                              |
    |                                |-- HotlinkOpen(path=_mpc/1_to_2) -->|                                |                              |
    |                                |                                  |-- HotlinkOpen (ACL check) ------>|                              |
    |                                |                                  |                                |-- bind TCP listener          |
    |                                |                                  |<-- HotlinkAccept(sid) ----------|                              |
    |                                |<-- HotlinkAccept(sid) ------------|                                |                              |
    |                                |                                  |                                |                              |
    |                                |== QUIC offer/answer (via HotlinkSignal through server) ==========>|                              |
    |                                |<=========================== QUIC connection established ==========>|                              |
    |                                |                                  |                                |                              |
    |-- TCP send(data) ------------->|                                  |                                |                              |
    |                                |-- HotlinkData(seq=1) via QUIC =================================>|                              |
    |                                |                                  |                                |-- reorder buf -> TCP write ->|
    |                                |                                  |                                |                              |-- TCP recv(data)
```

**Transport priority:** QUIC preferred → WS fallback (unless `QUIC_ONLY=1`)

### Protocol Messages (syftmsg types)

| Type | ID | Purpose |
|------|----|---------|
| `MsgHotlinkOpen` | 9 | Sender opens session for a path |
| `MsgHotlinkAccept` | 10 | Receiver accepts session |
| `MsgHotlinkReject` | 11 | Receiver rejects session |
| `MsgHotlinkData` | 12 | Payload frame with session_id + seq |
| `MsgHotlinkClose` | 13 | Close session |
| `MsgHotlinkSignal` | 14 | QUIC signaling (offer/answer/candidates) |

### Session Semantics
- **Path-scoped:** session bound to a directory like `_mpc/0_to_1`
- **IPC modes:**
  - `stream.sock` - UNIX domain socket (Linux/macOS)
  - `stream.tcp.request` - TCP proxy marker (for Syqure TCP proxy mode)
  - `stream.pipe` - Windows named pipe (not yet implemented)
- **TCP proxy mode:** Sequre connects to local TCP ports; SyftBox proxies the bytes over hotlink
- **No file fallback in TCP mode** (correctness > durability)

### Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `SYFTBOX_HOTLINK` | `0` | Enable hotlink |
| `SYFTBOX_HOTLINK_SOCKET_ONLY` | `0` | Use UNIX socket IPC only (no TCP proxy) |
| `SYFTBOX_HOTLINK_TCP_PROXY` | `0` | Enable TCP proxy for Syqure channels |
| `SYFTBOX_HOTLINK_QUIC` | `1` | Enable QUIC transport |
| `SYFTBOX_HOTLINK_QUIC_ONLY` | `0` | Disable WS fallback (fail if QUIC unavailable) |
| `SYFTBOX_HOTLINK_STUN_SERVER` | (none) | STUN server for NAT traversal |

## What's Done

### Phase 1: Hotlink over WebSocket + Local IPC ✅
- Protocol messages implemented (Open/Accept/Reject/Data/Close)
- Server routing with ACL-based accept/reject
- UNIX socket IPC in both Go and Rust clients
- E2E hotlink-protocol benchmark test

### Phase 1.5: TCP Tunnel over Hotlink ✅
- TCP proxy discovery via `stream.tcp.request` marker files
- Per-channel TCP listener binding and port mapping
- `getTCPWriterWithRetry` for writer readiness (handles race where data arrives before TCP connection)
- `TcpReorderBuf` in Rust client to handle out-of-order server relay (BTreeMap-based)
- Hard fail on send error (close TCP socket, no silent corruption)
- **Rust client:** Fully working and tested
- **Go client:** TCP proxy implemented but missing reorder buffer

### Phase 2: QUIC Peer-to-Peer Transport ✅ (localhost/LAN)
- QUIC transport via `quinn` (Rust) and `quic-go` (Go)
- WS signaling for QUIC offer/answer via `MsgHotlinkSignal` (relayed by server)
- Self-signed TLS certs generated per session (`rcgen` in Rust)
- Mixed transport: some peers on QUIC, others on WS fallback
- QUIC-only mode for testing
- Telemetry: tx/rx packets, bytes, quic/ws split, latency metrics
- Initial STUN candidate discovery code in both Go and Rust

### Server Changes ✅
- `handleHotlinkSignal` relay for QUIC signaling (new in this branch)
- All hotlink message types routed (Open, Accept, Reject, Data, Close, Signal)
- Concurrent worker goroutines relay messages (causes out-of-order delivery, handled by client reorder buffer)

### Critical Bugs Fixed ✅
1. **`notify_waiters()` race** (Rust): Changed to `notify_one()` - notifications were lost because accept arrived before `notified()` was polled
2. **TCP reorder buffer** (Rust): Server concurrent workers reorder HotlinkData; BTreeMap buffers and flushes in sequence order
3. **TCP writer readiness** (Rust): `getTCPWriterWithRetry` retries instead of dropping early frames
4. **IPC fallthrough** (Rust): TCP proxy paths no longer fall through to UNIX socket IPC
5. **Listener recreation race** (Rust): Create UNIX socket listener once at startup, not per-loop
6. **Eager IPC listener** (Rust): `ensure_ipc_listener()` during `handle_open` (not lazy in `handle_data`)
7. **RwLock deadlock** (Rust): Scope read guard to drop before write lock acquisition
8. **Codon JIT SIGSEGV** (Sequre): Split hotlink IPC into separate `hotlink_transport.codon` module

### Test Results (2026-02-07)
- **Scenario:** `syqure-distributed.yaml` with `BV_DEVSTACK_CLIENT_MODE=rust`
- **Result:** All 3 Sequre parties completed, MPC result `[3,3,4]` (correct)
- **Duration:** Aggregator ~40s, clients ~45s
- **Transport:** QUIC preferred with WS fallback (q17198/ws26, q17101/ws51)
- **Reproducible:** Passed on consecutive runs

### Benchmark Results (hotlink-protocol test, IPC round-trip)
| Metric | Go | Rust |
|--------|-----|------|
| P50 | ~330us | ~970us |
| P90 | ~870us | ~4.8ms |
| P95 | ~880us | ~5.5ms |
| P99 | ~1.1ms | ~5.8ms |

## What's Left To Do

### Required for Production

1. **Server deployment** - Deploy server with `MsgHotlinkSignal` (type 14) support. Without this, QUIC negotiation fails silently and clients fall back to WS-only.

2. **Go client reorder buffer** - Port `TcpReorderBuf` from Rust to Go for `BV_DEVSTACK_CLIENT_MODE=go` parity. The Go client will hit the same out-of-order TCP corruption without this.

3. **Integration test in syftbox repo** - Add a hotlink-specific test alongside existing sbdev integration tests. Should test at minimum:
   - Hotlink session open/accept/close lifecycle
   - TCP proxy data flow with reorder verification
   - QUIC upgrade and WS fallback

4. **Aggregator telemetry** - Aggregator connections still show "pending" in telemetry despite scenario passing. Investigate whether this is a reporting bug or actual issue.

### Required for Internet / NAT

5. **STUN real-network testing** - STUN candidate discovery code exists but is untested on real networks. Test on actual NAT to measure success rate.

6. **ICE candidate negotiation** - Currently no candidate pair testing or priority ordering. If STUN alone doesn't work for most NATs, implement basic ICE.

7. **TURN relay fallback** - For symmetric NAT where hole-punching fails. May not be needed depending on STUN success rate.

8. **UDP hole-punching** - No explicit NAT binding refresh or hole-punch logic yet.

### Nice to Have

9. **Rust client latency optimization** - Go has ~3-5x lower per-message latency. Opportunities: channel-based message passing, `parking_lot` mutexes, reduce async runtime overhead.

10. **Windows named pipe support** (`stream.pipe`) - Not implemented in either client.

11. **TCP IPC mode for containers** - `stream.tcp` marker for container compatibility where UNIX sockets don't work.

12. **File fallback replay** - Write `.request` files on hotlink failure, replay into IPC when hotlink recovers.

13. **OTEL tracing** - Per-stage latency spans for detailed observability.

14. **Bundle rebuild** - Rebuild syqure bundle tarball with updated sequre stdlib so new installs don't need manual cache patching.

## Files Changed (This Branch)

| File | Lines | What |
|------|-------|------|
| `rust/src/hotlink_manager.rs` | +1680 | QUIC, TCP proxy, reorder buffer, telemetry, STUN, notify_one fix |
| `internal/client/sync/hotlink_manager.go` | +1004 | QUIC, TCP proxy, telemetry, STUN (no reorder buffer) |
| `internal/server/server.go` | +33 | `handleHotlinkSignal` relay |
| `internal/syftmsg/msg_hotlink.go` | +22 | `HotlinkSignal` struct + constructor |
| `internal/syftmsg/msg_type.go` | +3 | `MsgHotlinkSignal = 14` |
| `internal/syftmsg/msg.go` | +6 | Unmarshal case for HotlinkSignal |
| `internal/wsproto/codec.go` | +15 | Msgpack marshal/unmarshal for HotlinkSignal |
| `rust/src/wsproto.rs` | +58 | HotlinkSignal encode/decode |
| `rust/Cargo.toml` | +3 | quinn, rcgen, rustls deps |
| `rust/src/client.rs` | +4 | WS dispatch for HotlinkSignal |
| `rust/src/hotlink.rs` | +1 | Minor |
| `rust/src/sync.rs` | +18 | TCP proxy wiring |
| `internal/client/sync/sync_engine.go` | +3 | Minor wiring |
| `internal/client/sync/acl_staging.go` | +1 | Grace window |

## Running

```bash
# Run distributed scenario (from biovault dir)
cd biovault && go run ./cmd/devstack scenario run syqure-distributed.yaml

# Run hotlink-protocol benchmark
./benchmark.sh --bench=hotlink-protocol --lang=rust
./benchmark.sh --bench=hotlink-protocol --lang=go
```
