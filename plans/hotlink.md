# Hotlink Plan

## Goals
- Provide the **lowest-latency** data path between peers for time‑critical MPC/HE workloads.
- Keep existing file-based durability as an optional fallback (do not slow the primary path).
- Keep the **application interface stable** (e.g., always read from a FIFO), even if hotlink drops.
- Support both Go and Rust clients; enable devstack E2E latency benchmarking.

## Current Baselines (Implemented)
- **Local watcher latency** baselines:
  - Go: `internal/client/sync/hotlink_baseline_test.go`
  - Rust: `rust/tests/hotlink_baseline.rs`
- **End‑to‑end latency baseline** (priority RPC files via devstack):
  - Go integration test: `cmd/devstack/hotlink_latency_test.go`
  - Run via `./benchmark.sh --bench=e2e-latency --lang=go|rust`
- **Hotlink protocol E2E test** (IPC → WS → IPC round-trip):
  - Integration test: `cmd/devstack/hotlink_protocol_test.go`
  - Run via `./benchmark.sh --bench=hotlink-protocol --lang=go|rust`
  - Tests 1KB, 10KB, 100KB payloads with latency metrics

## Phased Delivery

### Phase 0: Baseline & Observability (Now)
- Maintain baseline tests and scripts.
- Optional: add OTEL spans (client + server) for precise per-stage latency.
  - Span suggestions:
    - `watcher.detected`
    - `priority.send`
    - `server.handle_file_write`
    - `server.broadcast`
    - `client.receive_ws`
    - `client.write_file`

### Phase 1: Hotlink over WebSocket + Local IPC (Fastest to Ship)
**Objective:** Create a low‑latency streaming channel using existing WS transport (ordered, reliable) plus a reliable local IPC interface for apps, with file fallback.

- **Protocol (new syftmsg types)**
  - `MsgHotlinkOpen` / `MsgHotlinkAccept` / `MsgHotlinkReject`
  - `MsgHotlinkData` (session_id, seq, payload)
  - `MsgHotlinkClose`

- **Session semantics**
  - Path‑scoped: session bound to a directory like `_mpc/0_to_1`.
  - Primary sink is a local IPC endpoint inside the directory:
    - Linux/macOS: `stream.sock` (UNIX domain socket).
    - Windows native: `stream.pipe` (named pipe endpoint name derived from path).
    - Windows + Linux container: optional TCP IPC (host listener) for container compatibility.
  - Optional file fallback: only if hotlink is down.

- **Client behavior**
  - Create and listen on local IPC endpoint (UNIX socket on Linux/macOS, named pipe on Windows, or TCP when enabled).
  - Send data over WS with sequence numbers.
  - Write to IPC immediately; do not block on disk.
  - If hotlink drops, write/consume `.request` files and **replay into IPC** so apps keep reading from the stream.

- **Server behavior**
  - Accept/reject hotlink based on ACLs for the target path.
  - Route `HotlinkData` frames to subscribed peers.

### Phase 2: Optional Peer‑to‑Peer Transport
**Objective:** Lower latency further (avoid server hop) once Phase 1 is stable.

- P2P via QUIC/WebRTC with WS signaling.
- Keep same session API; transport switches are internal.
- Preserve IPC + file fallback behavior.

## Application Interface (MPC/HE)
- Directory layout example:
  - `_mpc/0_to_1/stream.sock` (Linux/macOS) or `_mpc/0_to_1/stream.pipe` (Windows native) as primary stream
  - `_mpc/0_to_1/stream.tcp` (optional marker/config for TCP IPC when container clients are used)
  - `_mpc/0_to_1/00000001.request` (fallback files)
- App reads the IPC stream (no polling). If hotlink fails, data is replayed from files into the stream by the client.

## Open Questions
- Decide when to enable file fallback automatically vs. manual.
- Decide whether to tee files asynchronously (mode 2) or write only on failure (mode 1).
- Decide whether to implement OTEL tracing now or after Phase 1.

## Next Steps
1. ✅ ~~Implement Hotlink message types and client/server routing (Phase 1).~~
2. ✅ ~~Add IPC implementation in Go and Rust clients (UNIX socket).~~
3. ✅ ~~Add E2E hotlink latency test case (hotlink-protocol benchmark).~~
4. Optimize Rust client latency to match Go performance.
5. Add Windows named pipe support (`stream.pipe`).
6. Add TCP IPC mode for container compatibility.
7. Implement file fallback replay (write `.request` files on hotlink failure).
8. Optional: Add OTEL tracing spans for detailed latency analysis.

## Current Status

### Go Client
- Socket‑only hotlink IPC is wired and benchmarked.
- TCP IPC mode is implemented behind:
  - `SYFTBOX_HOTLINK_IPC=tcp`
  - `SYFTBOX_HOTLINK_TCP_ADDR=host:port` (default `127.0.0.1:0`)
- Benchmark selects socket‑only by default (`SYFTBOX_HOTLINK_SOCKET_ONLY=1` set in test).

### Rust Client
- Socket-only hotlink IPC is now fully implemented and passing E2E tests.
- Implementation in `rust/src/hotlink_manager.rs` and `rust/src/hotlink.rs`.

### Benchmark Results (hotlink-protocol test)
| Metric | Go | Rust |
|--------|-----|------|
| P50 | ~330µs | ~970µs |
| P90 | ~870µs | ~4.8ms |
| P95 | ~880µs | ~5.5ms |
| P99 | ~1.1ms | ~5.8ms |

Go currently has ~3-5x lower latency per-message. Rust overhead is due to async runtime and lock patterns.

### Running Benchmarks
```bash
# Go client
./benchmark.sh --bench=hotlink-protocol --lang=go

# Rust client
./benchmark.sh --bench=hotlink-protocol --lang=rust
```

## Rust Implementation Notes

### Key Fixes (2025-01-28)
Three bugs were fixed to get the Rust hotlink implementation working:

1. **Listener recreation race condition** (`run_local_reader`)
   - **Problem:** The original code recreated the Unix socket listener on every loop iteration after accept timeout, which could remove the socket file while a client was connecting.
   - **Fix:** Create the listener once at startup and reuse it for all connections.

2. **Eager IPC listener creation** (`handle_open`)
   - **Problem:** The receiver IPC listener was only created lazily during `handle_data`, which was too late for clients that connect immediately after the hotlink session opens.
   - **Fix:** Added `ensure_ipc_listener()` method called during `handle_open` to create the listener early (matches Go's `EnsureListener()` pattern).

3. **RwLock deadlock** (`send_hotlink`)
   - **Problem:** The pattern `if let Some(id) = self.map.read().await.get(&key) { ... } else { self.map.write().await... }` holds the read guard across both branches in Rust, causing deadlock when acquiring the write lock.
   - **Fix:** Explicitly scope the read guard to drop it before the else branch:
   ```rust
   let existing = {
       let guard = self.map.read().await;
       guard.get(&key).cloned()
   };
   if let Some(id) = existing { ... } else { ... }
   ```

### Performance Optimization Opportunities
- Replace spawned tasks with channel-based message passing
- Reduce lock contention in `outbound` and `outbound_by_path` maps
- Consider using `parking_lot` mutexes for sync sections
- Profile async runtime overhead

## Implementation Notes / Open Design Thinking
- Windows + Linux container IPC:
  - Linux containers cannot open `\\.\pipe\...`; named pipes are Windows‑only objects.
  - For container producers, prefer **TCP IPC** to a host listener (e.g., `host.docker.internal:PORT`).
  - Keep named pipes for native Windows apps; add TCP as an optional mode.
- IPC modes to support:
  - `stream.sock` for unix.
  - `stream.pipe` for Windows native.
  - `stream.tcp` marker/config when TCP IPC is enabled (container use).
- Socket‑only hotlink:
  - Allow apps to write framed hotlink messages directly to IPC without touching `.request` files.
  - Client reads IPC frames → sends HotlinkData over WS → receiver writes to IPC.
  - Optional fallback: replay `.request` files into IPC on hotlink failure.
- Benchmarking:
  - Add env toggles to select IPC mode:
    - `SYFTBOX_HOTLINK_SOCKET_ONLY=1` (local IPC).
    - `SYFTBOX_HOTLINK_IPC=tcp` + `SYFTBOX_HOTLINK_TCP_ADDR=...` (container path).
  - Benchmark should send frames directly to IPC when socket‑only is enabled, and use file path when disabled.
