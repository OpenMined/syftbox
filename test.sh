#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "=== Go Tests ==="
go test -v -cover ./...

if [ -d rust ]; then
  echo ""
  echo "=== Rust Tests ==="
  cd rust
  # NOTE: Some tests have isolation issues and fail with parallel execution.
  # Using --test-threads=1 ensures reliable results.
  cargo test -- --test-threads=1
fi
