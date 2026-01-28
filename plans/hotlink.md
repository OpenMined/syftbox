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
1. Confirm OTEL tracing scope and env gating.
2. Implement Hotlink message types and client/server routing (Phase 1).
3. Add IPC + fallback replay implementation in Go and Rust clients:
   - Go: UNIX socket (linux/macOS) + named pipe (windows).
   - Rust: UNIX socket (linux/macOS) + named pipe (windows).
4. Add E2E hotlink latency test case (new benchmark mode).

## Current Status (Go)
- Socket‑only hotlink IPC is wired and benchmarked.
- TCP IPC mode is implemented behind:
  - `SYFTBOX_HOTLINK_IPC=tcp`
  - `SYFTBOX_HOTLINK_TCP_ADDR=host:port` (default `127.0.0.1:0`)
- Benchmark selects socket‑only by default (`SYFTBOX_HOTLINK_SOCKET_ONLY=1` set in test).

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
