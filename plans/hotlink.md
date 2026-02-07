# Hotlink Plan

## Goals
- Provide the **lowest-latency** data path between peers for time‑critical MPC/HE workloads.
- Keep existing file-based durability as an optional fallback (do not slow the primary path).
- Keep the **application interface stable** (e.g., always read from a FIFO), even if hotlink drops.
- Support both Go and Rust clients; enable devstack E2E latency benchmarking.
- Enable a **low-latency TCP tunnel** mode for Syqure-style socket streams (still carried over WS).

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

### Phase 1.5: TCP Tunnel over Hotlink (Syqure Distributed)
**Objective:** Provide a stream‑oriented, ordered, reliable tunnel that looks like TCP to apps, but is carried over the hotlink WS path.

- **TCP tunnel semantics**
  - One hotlink session per MPC channel path (e.g. `_mpc/1_to_2`).
  - **Single writer queue per channel** to preserve order and apply backpressure.
  - No per‑chunk open/accept; keep the session open for the lifetime of the TCP stream.
  - If send fails, **close the TCP socket** (hard fail) to avoid silent corruption.
  - Optional: configurable chunk size (e.g. 256KB) to reduce framing overhead.

- **Proxy behavior**
  - On local TCP accept, stream bytes → enqueue to hotlink channel worker.
  - On hotlink receive, write bytes to the mapped TCP writer in order.
  - No file fallback for TCP mode (correctness > durability).

- **Implementation constraints**
  - Must be implemented in **both Go and Rust clients** with identical semantics.
  - WS transport remains the carrier (no new transport required).

### Phase 2: Optional Peer‑to‑Peer Transport
**Objective:** Lower latency further (avoid server hop) once Phase 1 is stable.

- P2P via **QUIC** with WS signaling (implemented in both Go + Rust clients).
- Keep same session API; transport switches are internal.
- Preserve IPC + file fallback behavior.
- Allow **mixed transport**: some peers on QUIC, others on WS fallback in the same flow.
- Add a **QUIC-only** mode to force failure instead of fallback (debug/perf testing).

## Application Interface (MPC/HE)
- Directory layout example:
  - `_mpc/0_to_1/stream.sock` (Linux/macOS) or `_mpc/0_to_1/stream.pipe` (Windows native) as primary stream
  - `_mpc/0_to_1/stream.tcp` (optional marker/config for TCP IPC when container clients are used)
  - `_mpc/0_to_1/00000001.request` (fallback files)
- App reads the IPC stream (no polling). If hotlink fails, data is replayed from files into the stream by the client.
- For TCP tunnel mode, app connects to local TCP proxy; hotlink ensures ordered delivery.

## Open Questions
- Decide when to enable file fallback automatically vs. manual.
- Decide whether to tee files asynchronously (mode 2) or write only on failure (mode 1).
- Decide whether to implement OTEL tracing now or after Phase 1.

## Next Steps
1. ✅ ~~Implement Hotlink message types and client/server routing (Phase 1).~~
2. ✅ ~~Add IPC implementation in Go and Rust clients (UNIX socket).~~
3. ✅ ~~Add E2E hotlink latency test case (hotlink-protocol benchmark).~~
4. ✅ ~~Implement TCP tunnel mode (Phase 1.5) - Rust client working, Go client partial.~~
5. ✅ ~~QUIC transport with WS fallback (Phase 2) - implemented in both Go and Rust.~~
6. ✅ ~~Distributed scenario passing with TCP proxy + QUIC (~60s).~~
7. Port TCP reorder buffer from Rust to Go client for `BV_DEVSTACK_CLIENT_MODE=go` parity.
8. Add hotlink-specific integration test to syftbox repo (alongside existing sbdev tests).
9. Test STUN candidate discovery over real NAT networks.
10. Add Windows named pipe support (`stream.pipe`).
11. Add TCP IPC mode for container compatibility.
12. Implement file fallback replay (write `.request` files on hotlink failure).
13. Optional: Add OTEL tracing spans for detailed latency analysis.
14. Optional: ICE/TURN for symmetric NAT traversal (pending STUN test results).

## Immediate Internet Bring-Up (2026-02-06)
**Goal:** Get quick real-network results with minimal complexity and keep WS fallback safety.

1. Make `transport: hotlink` the default operator path in BioVault flows.
2. Default hotlink behavior to:
   - QUIC preferred
   - WS fallback enabled
   - QUIC-only only when explicitly requested
3. Keep TCP proxy enabled by default for Syqure distributed channels.
4. Add first-pass hotlink telemetry for packet/byte/send-latency visibility.
5. Implement STUN-assisted candidate discovery (Go + Rust) as the first NAT traversal step.
6. Measure real-network success rate and fallback rate before deciding on ICE/TURN scope.

## Current Status

### BioVault / Flow Defaults
- `transport: hotlink` is now treated as the primary path for Syqure modules.
- `SEQURE_TCP_PROXY` now defaults to enabled when transport is hotlink (can still be disabled explicitly).
- `test-scenario.sh` hotlink mode now defaults to:
  - `BV_SYFTBOX_HOTLINK=1`
  - `BV_SYFTBOX_HOTLINK_QUIC=1`
  - `BV_SYFTBOX_HOTLINK_QUIC_ONLY=0` (unless `--hotlink-quic-only` is passed)
- Scenario setup defaults now align with quick-start behavior: hotlink on, TCP proxy on, QUIC on.

### Syqure Runtime Defaults
- Syqure runner now prefers bundled Codon/Sequre assets by default.
- Local Codon installs are now fallback/debug paths, instead of required baseline setup.
- `SYQURE_SKIP_BUNDLE` is now treated as an explicit override rather than normal operation.

### Go Client
- Socket‑only hotlink IPC is wired and benchmarked.
- TCP IPC mode is implemented behind:
  - `SYFTBOX_HOTLINK_IPC=tcp`
  - `SYFTBOX_HOTLINK_TCP_ADDR=host:port` (default `127.0.0.1:0`)
- Benchmark selects socket‑only by default (`SYFTBOX_HOTLINK_SOCKET_ONLY=1` set in test).
- QUIC transport implemented with WS signaling:
  - Env toggles:
    - `SYFTBOX_HOTLINK_QUIC=1` enable QUIC
    - `SYFTBOX_HOTLINK_QUIC_ONLY=1` forbid WS fallback
  - Logs announce QUIC offer/answer and fallback usage.

### Rust Client
- Socket-only hotlink IPC is now fully implemented and passing E2E tests.
- Implementation in `rust/src/hotlink_manager.rs` and `rust/src/hotlink.rs`.
- QUIC transport implemented (same env toggles and semantics as Go).
- rustls provider is explicitly installed to avoid runtime panics.
- Added initial hotlink telemetry counters and periodic flush to:
  - `~/.syftbox/hotlink_telemetry.json` (within the active datasite sandbox)
  - includes tx/rx packets, bytes, quic/ws split, avg/max send/write ms, offer/answer/fallback counters.

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
- QUIC transport:
  - QUIC sessions are negotiated via WS signaling (`HotlinkSignal`).
  - QUIC transport is preferred when enabled; WS is used as a fallback unless QUIC-only.
  - Mixed transport is supported (some peers QUIC, others WS).
  - Add explicit logging when fallback occurs so operators can see path selection.
  - Current internet limitation: candidate addresses are still local/bind-IP centric.
  - Next increment is STUN-derived server-reflexive candidates (quick NAT traversal win).
- Benchmarking:
  - Add env toggles to select IPC mode:
    - `SYFTBOX_HOTLINK_SOCKET_ONLY=1` (local IPC).
    - `SYFTBOX_HOTLINK_IPC=tcp` + `SYFTBOX_HOTLINK_TCP_ADDR=...` (container path).
  - Benchmark should send frames directly to IPC when socket‑only is enabled, and use file path when disabled.

## Progress Dump (2026-02-06, late)
This section reflects the full session log and supersedes any optimistic status above.

### Landed Changes
- BioVault run output now prints explicit transport mode and hotlink flags per Syqure step.
- BioVault status output is mode-aware:
  - file/legacy mode: file/age channel counters.
  - hotlink mode: `q/ws/fb/avg-ms` telemetry summary when available.
- Scenario/default plumbing was simplified toward:
  - `transport: hotlink`
  - TCP proxy enabled by default for hotlink path
  - QUIC preferred, WS fallback enabled by default
- SyftBox telemetry file writing was added in Rust hotlink manager (`hotlink_telemetry.json`).
- STUN candidate discovery work was added in both Go and Rust hotlink managers (initial implementation stage).

### Runtime Reality From Repeated Runs
- The scenario can complete successfully around the expected fast window (~55s Syqure step) on some runs.
- The same default command also reproduces intermittent failure on other runs:
  - aggregator `signal: 6` around ~25s in secure aggregate phase, then other parties stall/hang.
- Log evidence seen during failures included:
  - `hotlink ipc accept timeout`
  - `hotlink ipc write failed`
  - occasional telemetry lines staying `pending` / zeros for peers.

### Investigated but Not Yet Confirmed as Root Cause
- ACL grace-window expiry was observed for staged ACL files; grace was extended to 10 minutes in Go/Rust staging code.
- That ACL extension did **not** eliminate the `signal: 6` regression in all runs.
- A TCP channel port-mapping asymmetry for reverse direction channels was identified and patched in BioVault flow channel math.
- Additional verification is still required to prove this removes the crash class.

### Current Risk / Readiness
- Status: **not yet CI-ready as a reliable default** despite partial successful runs.
- Required before green-light:
  1. Reproduce stability over multiple consecutive default runs (`./test-scenario.sh tests/scenarios/syqure-distributed.yaml`).
  2. Confirm hotlink telemetry consistently reports non-pending counters for active parties.
  3. Confirm no aggregator `signal: 6` across repeated runs (with and without QUIC).
  4. Keep WS fallback path healthy while iterating on QUIC/STUN behavior.

## Latest Debug Update (2026-02-06)

### What Was Confirmed
- The scenario uses native Syqure at:
  - `biovault/../syqure/target/release/syqure`
- With `SYQURE_SKIP_BUNDLE=1`, Syqure TCP port mapping is correct per party:
  - CP0 uses `900x`
  - CP1 uses `1000x`
  - CP2 uses `1100x`
- Without forcing skip-bundle, prior runs showed stale stdlib behavior (clients using `900x`), which can still reappear depending on active bundle/codon asset path.

### Reproduced Current Blocker (Still Failing)
- Reproduced hang in `secure_aggregate` both:
  - QUIC preferred + WS fallback
  - WS-only (`BV_SYFTBOX_HOTLINK_QUIC=0`)
- All three Syqure processes remain alive but stalled in `secure_aggregate` for long periods.
- Hotlink telemetry often remains near-zero during the hang, indicating traffic is not flowing end-to-end.

### Root-Cause Signal From Logs
- On receiver side (e.g. `client1`), first TCP hotlink frame arrives before local TCP writer is mapped:
  - `hotlink tcp write skipped: no writer for path=.../stream.tcp.request`
- After skip, code falls through into IPC socket path for the same TCP session:
  - repeated `hotlink ipc accept retry .../stream.sock`
  - then `hotlink ipc write failed: ipc accept timeout after 30s`
- This indicates incorrect handling for TCP proxy sessions:
  - TCP frames are dropped when writer is not ready.
  - TCP mode then incorrectly attempts UNIX-socket IPC fallback (`stream.sock`), which is not valid for this path.

### Why This Causes Deadlock
- The first control/data bytes on a channel can be lost before writer mapping is ready.
- Once those bytes are dropped, MPC peers wait forever on missing messages.
- This matches observed behavior: all parties remain in `secure_aggregate` with no completion.

### Immediate Fix Direction (Next Patch Target)
1. In `syftbox/rust/src/hotlink_manager.rs`, for `is_tcp_proxy_path()` frames:
   - never fall through to `write_ipc(stream.sock)` when no TCP writer exists.
2. Add bounded buffering/retry for early TCP frames until writer mapping appears.
3. If writer never appears within timeout, fail channel hard and close session/socket explicitly (do not silently drop).
4. Add metrics/log counters for:
   - `tcp_writer_not_ready`
   - `tcp_frame_buffered`
   - `tcp_frame_dropped_timeout`

### Scenario Harness Note
- `biovault/tests/scenarios/syqure-distributed.yaml` setup line was adjusted to avoid nested `${...:-...}` expansion syntax that the current scenario parser intermittently misread; equivalent defaults now use `${VAR-default}` form.

## Debug Update (2026-02-07, earlier)

### SIGSEGV Root Cause Identified and Fixed

**Problem:** `cargo run -p syqure -- example/two_party_sum.codon` crashed with SIGSEGV (signal 11) at "CP1: MHE generating relinearization key". The aggregator dying during MHE key generation killed the entire distributed scenario.

**Root cause:** A Codon LLVM JIT optimization bug. Adding 233+ lines of pointer-heavy hotlink IPC code directly into `file_transport.codon` corrupts unrelated compiled code in the MHE module.

**Fix:** Split hotlink IPC code into a separate `hotlink_transport.codon` module with lazy conditional imports. In TCP proxy mode, `run_dynamic.rs` sets `SEQURE_TRANSPORT=tcp` so the hotlink IPC code is never compiled.

### Commits (Sequre/Syqure side)
- **Sequre submodule** (`d3bdcdb`): Split hotlink transport into separate module
- **Syqure** (`84e7835`): Updated sequre submodule pointer
- **Syqure** (`295d3e9`): Disabled OutputCapture in bridge.cc to prevent fork() SIGABRT

## Latest Update (2026-02-07) - Distributed Scenario PASSING

### Two Critical Bugs Fixed in Rust Client

Both fixes are in `rust/src/hotlink_manager.rs` (client-side only, no server changes needed for these):

#### Fix 1: `notify_waiters()` → `notify_one()` Race Condition
- **Problem:** `tokio::sync::Notify::notify_waiters()` does NOT buffer notifications. On localhost, `HotlinkAccept` round-trips so fast that it arrives and is processed in the gap between dropping the read lock (after checking `accepted=false`) and polling the `notified()` future in `wait_for_accept`. Every hotlink session timed out after 1.5s, leaving all connections permanently "pending".
- **Fix:** Changed `notify_waiters()` → `notify_one()` in both `handle_accept` and `handle_reject`. `notify_one()` buffers a single permit that is consumed when `notified()` is later polled.
- **Lines:** 771 (`handle_accept`), 788 (`handle_reject`)

#### Fix 2: TCP Reorder Buffer for Out-of-Order HotlinkData
- **Problem:** The Go server uses `runtime.NumCPU()` concurrent worker goroutines reading from a shared message channel. HotlinkData messages within a session are processed by different workers and relayed out of order (e.g., seq=4 before seq=3). Writing TCP data out of order corrupts the byte stream, causing Sequre to segfault (signal 11).
- **Fix:** Added `TcpReorderBuf` struct with `BTreeMap<u64, Vec<u8>>` per TCP proxy path. `handle_frame` inserts incoming frames by sequence number, then flushes consecutive frames starting from `next_seq` in order.
- **Struct:**
  ```rust
  struct TcpReorderBuf {
      next_seq: u64,
      pending: BTreeMap<u64, Vec<u8>>,
  }
  ```

### Test Results
- **Scenario:** `syqure-distributed.yaml` with `BV_DEVSTACK_CLIENT_MODE=rust`
- **Result:** All 3 Sequre parties completed, MPC aggregation result `[3,3,4]` (correct)
- **Duration:** ~60 seconds per party (meets target)
- **Transport:** QUIC preferred with WS fallback (telemetry: q16212/ws51)

### Deployment Requirements
- **Server:** YES, requires deployment for `MsgHotlinkSignal` (type 14) - new message type for QUIC signaling relay. Without this, QUIC negotiation fails and clients fall back to WS-only.
- **Rust client:** YES, contains both critical fixes above plus QUIC, TCP proxy, telemetry, and STUN support.
- **Go client:** Has QUIC signaling and TCP proxy code but does NOT yet have the reorder buffer fix. The Go client is not used by the current test (`BV_DEVSTACK_CLIENT_MODE=rust`).

### NAT / Internet Readiness
- **Current state:** Works on localhost and LAN only.
- **QUIC transport:** Uses direct IP addresses exchanged via `HotlinkSignal` messages through the server.
- **STUN:** Initial candidate discovery code exists in both Go and Rust clients but is not yet battle-tested.
- **Missing for NAT traversal:**
  1. STUN-derived server-reflexive candidates need testing on real networks
  2. No ICE (Interactive Connectivity Establishment) - no candidate pair negotiation
  3. No TURN relay fallback for symmetric NAT
  4. No hole-punching logic for UDP NAT binding
- **Recommendation:** STUN testing on real networks is the next step. If success rate is high, ICE/TURN may not be needed for most deployments.

### What Changed (Full Diff Summary)

| Area | Files | Changes |
|------|-------|---------|
| **Server** | `server.go` | +`handleHotlinkSignal` relay for QUIC signaling |
| **Protocol** | `msg_type.go`, `msg.go`, `msg_hotlink.go` | +`MsgHotlinkSignal` (type 14) with sid/kind/addrs/token/error |
| **Codec** | `wsproto/codec.go` | +msgpack marshal/unmarshal for HotlinkSignal |
| **Rust client** | `hotlink_manager.rs` (+1680 lines) | QUIC transport, TCP proxy, reorder buffer, telemetry, STUN, notify_one fix |
| **Rust deps** | `Cargo.toml`, `Cargo.lock` | +quinn, +rcgen, +rustls |
| **Rust proto** | `wsproto.rs` | +HotlinkSignal encode/decode |
| **Go client** | `hotlink_manager.go` (+1004 lines) | QUIC transport, TCP proxy, telemetry, STUN (no reorder buffer yet) |
| **Go misc** | `sync_engine.go`, `acl_staging.go` | Minor wiring changes |

### Remaining Work
1. **Go client reorder buffer**: Port the `TcpReorderBuf` from Rust to Go for parity when `BV_DEVSTACK_CLIENT_MODE=go`.
2. **Reproducibility**: Run scenario multiple times to confirm stability.
3. **Aggregator telemetry**: Aggregator connections still show "pending" in telemetry despite scenario passing - investigate.
4. **STUN real-network testing**: Test STUN candidate discovery over actual NAT.
5. **Bundle rebuild**: Rebuild syqure bundle tarball with updated sequre stdlib.
