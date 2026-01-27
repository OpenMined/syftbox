#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: ./benchmark.sh [--lang go|rust|both] [--bench watcher|e2e-latency|e2e-latency-hotlink] [--help]

Runs SyftBox baseline latency benchmarks.

Options:
  --lang   Which client(s) to run: go, rust, or both (default: both)
  --bench  Which benchmark to run (default: watcher)
           watcher = local file watcher latency baseline
           e2e-latency = devstack end-to-end latency baseline (priority RPC)
           e2e-latency-hotlink = devstack end-to-end latency with hotlink enabled
  --help   Show this help

Environment:
  SYFTBOX_HOTLINK_BENCH=1 is set automatically for the benchmark process.
USAGE
}

lang="both"
bench="watcher"

for arg in "$@"; do
  case "$arg" in
    --lang=*)
      lang="${arg#*=}"
      ;;
    --bench=*)
      bench="${arg#*=}"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown arg: $arg" >&2
      usage
      exit 1
      ;;
  esac
done

run_go() {
  case "$bench" in
    watcher)
      SYFTBOX_HOTLINK_BENCH=1 go test ./internal/client/sync -run HotlinkBaseline
      ;;
    e2e-latency)
      (
        cd cmd/devstack
        REPO_ROOT="$(pwd)/../.."
        if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
          case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
          esac
        else
          SANDBOX_DIR="$REPO_ROOT/.test-sandbox/perf-tests"
        fi
        echo "Using sandbox: $SANDBOX_DIR"
        if [ -d "$SANDBOX_DIR" ]; then
          chmod -R u+w "$SANDBOX_DIR" 2>/dev/null || true
        fi
        rm -rf "$SANDBOX_DIR" || true
        SYFTBOX_LATENCY_TRACE=1 PERF_TEST_SANDBOX="$SANDBOX_DIR" \
          go test -v -timeout 30m -tags integration -run '^TestHotlinkLatencyE2E$' -count=1
      )
      ;;
    e2e-latency-hotlink)
      (
        cd cmd/devstack
        REPO_ROOT="$(pwd)/../.."
        if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
          case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
          esac
        else
          SANDBOX_DIR="$REPO_ROOT/.test-sandbox/perf-tests"
        fi
        echo "Using sandbox: $SANDBOX_DIR"
        if [ -d "$SANDBOX_DIR" ]; then
          chmod -R u+w "$SANDBOX_DIR" 2>/dev/null || true
        fi
        rm -rf "$SANDBOX_DIR" || true
        SYFTBOX_LATENCY_TRACE=1 SYFTBOX_HOTLINK=1 PERF_TEST_SANDBOX="$SANDBOX_DIR" \
          go test -v -timeout 30m -tags integration -run '^TestHotlinkLatencyE2EHotlink$' -count=1
      )
      ;;
    *)
      echo "Unsupported --bench=$bench" >&2
      exit 1
      ;;
  esac
}

run_rust() {
  case "$bench" in
    watcher)
      (cd rust && SYFTBOX_HOTLINK_BENCH=1 cargo test -p syftbox-rs --test hotlink_baseline)
      ;;
    e2e-latency)
      (
        root_dir="$(pwd)"
        rust_bin="$root_dir/rust/target/release/syftbox-rs"
        (cd rust && cargo build --release)
        cd cmd/devstack
        REPO_ROOT="$(pwd)/../.."
        if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
          case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
          esac
        else
          SANDBOX_DIR="$REPO_ROOT/.test-sandbox/perf-tests"
        fi
        echo "Using sandbox: $SANDBOX_DIR"
        rm -rf "$SANDBOX_DIR"
        SYFTBOX_LATENCY_TRACE=1 SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" \
          go test -v -timeout 30m -tags integration -run '^TestHotlinkLatencyE2E$' -count=1
      )
      ;;
    e2e-latency-hotlink)
      (
        root_dir="$(pwd)"
        rust_bin="$root_dir/rust/target/release/syftbox-rs"
        (cd rust && cargo build --release)
        cd cmd/devstack
        REPO_ROOT="$(pwd)/../.."
        if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
          case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
          esac
        else
          SANDBOX_DIR="$REPO_ROOT/.test-sandbox/perf-tests"
        fi
        echo "Using sandbox: $SANDBOX_DIR"
        rm -rf "$SANDBOX_DIR"
        SYFTBOX_LATENCY_TRACE=1 SYFTBOX_HOTLINK=1 SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" \
          go test -v -timeout 30m -tags integration -run '^TestHotlinkLatencyE2EHotlink$' -count=1
      )
      ;;
    *)
      echo "Unsupported --bench=$bench" >&2
      exit 1
      ;;
  esac
}

case "$lang" in
  go)
    run_go
    ;;
  rust)
    run_rust
    ;;
  both)
    run_go
    run_rust
    ;;
  *)
    echo "Unsupported --lang=$lang (use go|rust|both)" >&2
    exit 1
    ;;
esac
