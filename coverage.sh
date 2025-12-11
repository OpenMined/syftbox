#!/usr/bin/env bash
set -euo pipefail

# Coverage runner for the Rust client (./rust) using cargo-llvm-cov.
# Generates HTML + LCOV and prints a summary.

# Parse flags
FULL_CLEAN_FLAG=0
OPEN_HTML_FLAG=${OPEN_HTML:-0}
for arg in "$@"; do
  case "$arg" in
    --full-clean|-c)
      FULL_CLEAN_FLAG=1
      ;;
    --open)
      OPEN_HTML_FLAG=1
      ;;
    --help|-h)
      echo "Usage: $0 [--full-clean|-c] [--open]";
      echo "  --full-clean, -c  Run cargo clean and remove coverage dirs before running";
      echo "  --open            Open HTML report locally (no-op in CI)";
      exit 0;
      ;;
    *) ;;
  esac
done

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
CRATE_DIR="$ROOT_DIR/rust"
cd "$CRATE_DIR"

echo "==> Formatting and linting (like test.sh)"
cargo fmt
cargo clippy --all-targets --all-features -q || true

echo "==> Checking cargo-llvm-cov availability"
if ! cargo llvm-cov --version >/dev/null 2>&1; then
  if [[ "${AUTO_INSTALL_LLVM_COV:-1}" == "1" ]]; then
    echo "==> Installing cargo-llvm-cov (first run only)"
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
    echo "==> Installing rustup component: llvm-tools-preview (first run only)"
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
  echo "==> FULL_CLEAN=1: performing cargo clean and removing coverage dirs"
  cargo clean
  rm -rf target/llvm-cov target/coverage target/llvm-cov-target || true
fi

echo "==> Cleaning previous coverage artifacts"
cargo llvm-cov clean --workspace

# Ensure coverage output directory exists after cleaning steps
mkdir -p target/coverage

LCOV_OUT=${LCOV_OUT:-target/coverage/lcov.info}
HTML_FLAG="--html"
OPEN_FLAG=""
if [[ "$OPEN_HTML_FLAG" == "1" ]]; then
  OPEN_FLAG="--open"
fi

echo "==> Running coverage"
# Detect if cargo-llvm-cov supports --nextest
NEXTEST_FLAG=""
if cargo llvm-cov --help 2>/dev/null | grep -q -e "--nextest"; then
  NEXTEST_FLAG="--nextest"
fi
HTML_DIR="target/llvm-cov/html"
cargo llvm-cov $NEXTEST_FLAG --workspace --all-features --html --output-dir "$HTML_DIR" $OPEN_FLAG

echo "==> Exporting LCOV (from existing coverage data)"
cargo llvm-cov report --lcov --output-path "$LCOV_OUT"

echo "==> Coverage summary (sorted by coverage %)"
# Capture the summary once to avoid broken pipe errors when truncating output.
SUMMARY_OUTPUT=$(cargo llvm-cov report --summary-only)
printf '%s\n' "$SUMMARY_OUTPUT" | head -n 3
printf '%s\n' "$SUMMARY_OUTPUT" | tail -n +4 | grep -v "^TOTAL" | sort -t'%' -k3 -n
printf '%s\n' "$SUMMARY_OUTPUT" | grep "^TOTAL"

# Best-effort path to HTML report (cargo-llvm-cov default)
HTML_DIR="target/llvm-cov/html"
if [[ -d "$HTML_DIR" ]]; then
  echo "HTML report: rust/$HTML_DIR/index.html"
else
  echo "HTML report directory not found. cargo-llvm-cov typically writes to target/llvm-cov/html" >&2
fi

echo "LCOV file: rust/$LCOV_OUT"
