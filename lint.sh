
#!/usr/bin/env bash
set -euo pipefail

cd src-tauri 2>/dev/null || true

if [ -d src-tauri ]; then
  echo "Linting src-tauri..."
  cargo fmt --all
  cargo clippy --fix --allow-dirty --all-targets --all-features --no-deps -- -D warnings
fi

if [ -d rust ]; then
  echo "Linting rust/..."
  cd rust
  cargo fmt --all
  cargo clippy --fix --allow-dirty --all-targets --all-features --no-deps -- -D warnings
fi
