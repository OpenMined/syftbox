#!/usr/bin/env bash
set -euo pipefail

# Coverage runner for SyftBox.
# - Rust client coverage (./rust) using cargo-llvm-cov.
# - Go coverage (repo root) using go test.
# Generates HTML + LCOV/coverprofile and prints a summary.

# Parse flags
FULL_CLEAN_FLAG=0
OPEN_HTML_FLAG=${OPEN_HTML:-0}
RUN_RUST=0
RUN_GO=0
for arg in "$@"; do
  case "$arg" in
    --rust)
      RUN_RUST=1
      ;;
    --go)
      RUN_GO=1
      ;;
    --full-clean|-c)
      FULL_CLEAN_FLAG=1
      ;;
    --open)
      OPEN_HTML_FLAG=1
      ;;
    --help|-h)
      echo "Usage: $0 [--rust] [--go] [--full-clean|-c] [--open]";
      echo "  --rust            Run Rust coverage (default if no mode specified)";
      echo "  --go              Run Go coverage";
      echo "  --full-clean, -c  Clean build/coverage artifacts before running";
      echo "  --open            Open HTML report locally (no-op in CI)";
      exit 0;
      ;;
    *) ;;
  esac
done

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [[ "$RUN_RUST" == "0" && "$RUN_GO" == "0" ]]; then
  RUN_RUST=1
fi

run_rust() {
  local CRATE_DIR="$ROOT_DIR/rust"
  cd "$CRATE_DIR"

  echo "==> [rust] Formatting and linting (like test.sh)"
  cargo fmt
  cargo clippy --all-targets --all-features -q || true

  echo "==> [rust] Checking cargo-llvm-cov availability"
  if ! cargo llvm-cov --version >/dev/null 2>&1; then
    if [[ "${AUTO_INSTALL_LLVM_COV:-1}" == "1" ]]; then
      echo "==> [rust] Installing cargo-llvm-cov (first run only)"
      if ! cargo install cargo-llvm-cov; then
        echo "Failed to install cargo-llvm-cov. Install manually with:" >&2
        echo "  cargo install cargo-llvm-cov" >&2
        exit 1
      fi
    else
      echo "cargo-llvm-cov is not installed. Install with:" >&2
      echo "  cargo install cargo-llvm-cov" >&2
      exit 1
    fi
  fi

  # Ensure llvm-tools only when running coverage, to avoid slowing CI in other jobs
  if ! rustup component list --installed | grep -q '^llvm-tools-preview'; then
    if [[ "${AUTO_INSTALL_LLVM_TOOLS:-1}" == "1" ]]; then
      echo "==> [rust] Installing rustup component: llvm-tools-preview (first run only)"
      if ! rustup component add llvm-tools-preview; then
        echo "Failed to install llvm-tools-preview. Install manually with:" >&2
        echo "  rustup component add llvm-tools-preview" >&2
        exit 1
      fi
    else
      echo "llvm-tools-preview is missing. Enable auto-install via AUTO_INSTALL_LLVM_TOOLS=1 or run:" >&2
      echo "  rustup component add llvm-tools-preview" >&2
      exit 1
    fi
  fi

  mkdir -p target/coverage

  if [[ "${FULL_CLEAN:-0}" == "1" || "$FULL_CLEAN_FLAG" == "1" ]]; then
    echo "==> [rust] FULL_CLEAN=1: performing cargo clean and removing coverage dirs"
    cargo clean
    rm -rf target/llvm-cov target/coverage target/llvm-cov-target || true
  fi

  echo "==> [rust] Cleaning previous coverage artifacts"
  cargo llvm-cov clean --workspace

  mkdir -p target/coverage

  LCOV_OUT=${LCOV_OUT:-target/coverage/lcov.info}
  OPEN_FLAG=""
  if [[ "$OPEN_HTML_FLAG" == "1" ]]; then
    OPEN_FLAG="--open"
  fi

  echo "==> [rust] Running coverage"
  NEXTEST_FLAG=""
  if cargo llvm-cov --help 2>/dev/null | grep -q -e "--nextest"; then
    NEXTEST_FLAG="--nextest"
  fi
  HTML_DIR="target/llvm-cov/html"
  cargo llvm-cov $NEXTEST_FLAG --workspace --all-features --html --output-dir "$HTML_DIR" $OPEN_FLAG

  echo "==> [rust] Exporting LCOV (from existing coverage data)"
  cargo llvm-cov report --lcov --output-path "$LCOV_OUT"

  echo "==> [rust] Coverage summary (sorted by coverage %)"
  SUMMARY_OUTPUT=$(cargo llvm-cov report --summary-only)
  printf '%s\n' "$SUMMARY_OUTPUT" | head -n 3
  printf '%s\n' "$SUMMARY_OUTPUT" | tail -n +4 | grep -v "^TOTAL" | sort -t'%' -k3 -n
  printf '%s\n' "$SUMMARY_OUTPUT" | grep "^TOTAL"

  if [[ -d "$HTML_DIR" ]]; then
    echo "HTML report: rust/$HTML_DIR/index.html"
  else
    echo "HTML report directory not found. cargo-llvm-cov typically writes to target/llvm-cov/html" >&2
  fi

  echo "LCOV file: rust/$LCOV_OUT"
}

run_go() {
  cd "$ROOT_DIR"
  mkdir -p .out

  if [[ "${FULL_CLEAN:-0}" == "1" || "$FULL_CLEAN_FLAG" == "1" ]]; then
    echo "==> [go] FULL_CLEAN=1: removing previous coverage artifacts"
    rm -f .out/coverage-go.out .out/coverage-go.html || true
  fi

  echo "==> [go] Running go test with coverage"
  go test ./... -count=1 -covermode=atomic -coverprofile=.out/coverage-go.out

  echo "==> [go] Coverage summary"
  go tool cover -func=.out/coverage-go.out | tail -n 20

  echo "==> [go] HTML report"
  go tool cover -html=.out/coverage-go.out -o .out/coverage-go.html
  echo "HTML report: .out/coverage-go.html"
}

if [[ "$RUN_RUST" == "1" ]]; then
  run_rust
fi
if [[ "$RUN_GO" == "1" ]]; then
  run_go
fi
