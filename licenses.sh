#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${ROOT_DIR}/licenses"

mkdir -p "${OUT_DIR}"
export OUT_DIR

INSTALL_MISSING=false
SCAN_TARGETS="all"
FETCH_LICENSES=false

usage() {
  cat <<'EOF'
Usage: ./licenses.sh [--install] [--scan all|go|rust|go,rust] [--fetch]

Options:
  --install   Install missing license tools for detected languages
  --scan      Restrict scans to a subset (default: all)
  --fetch     Fetch and cache license texts for Go modules (requires network)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install)
      INSTALL_MISSING=true
      shift
      ;;
    --scan)
      SCAN_TARGETS="${2:-}"
      shift 2
      ;;
    --fetch)
      FETCH_LICENSES=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1"
      usage
      exit 1
      ;;
  esac
done

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

scan_enabled() {
  local target="$1"
  if [[ "${SCAN_TARGETS}" == "all" ]]; then
    return 0
  fi
  IFS=',' read -r -a targets <<< "${SCAN_TARGETS}"
  for t in "${targets[@]}"; do
    if [[ "${t}" == "${target}" ]]; then
      return 0
    fi
  done
  return 1
}

report_go() {
  if [[ ! -f "${ROOT_DIR}/go.mod" ]]; then
    return
  fi

  if ! scan_enabled "go"; then
    return
  fi

  if ! has_cmd go; then
    echo "go not found; skipping Go license scan."
    return
  fi

  if has_cmd go-licenses; then
    echo "Scanning Go dependencies with go-licenses..."
    (cd "${ROOT_DIR}" && go-licenses csv ./... > "${OUT_DIR}/go-licenses.csv")
    echo "Go licenses written to ${OUT_DIR}/go-licenses.csv"
  else
    if [[ "${INSTALL_MISSING}" == "true" ]]; then
      echo "Installing go-licenses..."
      (cd "${ROOT_DIR}" && go install github.com/google/go-licenses@latest)
    else
      echo "go-licenses not found. Install with:"
      echo "  go install github.com/google/go-licenses@latest"
      return
    fi
    if has_cmd go-licenses; then
      echo "Scanning Go dependencies with go-licenses..."
      (cd "${ROOT_DIR}" && go-licenses csv ./... > "${OUT_DIR}/go-licenses.csv")
      echo "Go licenses written to ${OUT_DIR}/go-licenses.csv"
    else
      echo "go-licenses still not found after install attempt."
    fi
  fi
}

report_rust() {
  if [[ ! -f "${ROOT_DIR}/rust/Cargo.toml" ]]; then
    return
  fi

  if ! scan_enabled "rust"; then
    return
  fi

  if ! has_cmd cargo; then
    echo "cargo not found; skipping Rust license scan."
    return
  fi

  if has_cmd cargo-license; then
    echo "Scanning Rust dependencies with cargo-license..."
    (cd "${ROOT_DIR}/rust" && cargo license --json > "${OUT_DIR}/rust-licenses.json")
    echo "Rust licenses written to ${OUT_DIR}/rust-licenses.json"
  else
    if [[ "${INSTALL_MISSING}" == "true" ]]; then
      echo "Installing cargo-license..."
      (cd "${ROOT_DIR}/rust" && cargo install cargo-license)
    else
      echo "cargo-license not found. Install with:"
      echo "  cargo install cargo-license"
      return
    fi
    if has_cmd cargo-license; then
      echo "Scanning Rust dependencies with cargo-license..."
      (cd "${ROOT_DIR}/rust" && cargo license --json > "${OUT_DIR}/rust-licenses.json")
      echo "Rust licenses written to ${OUT_DIR}/rust-licenses.json"
    else
      echo "cargo-license still not found after install attempt."
    fi
  fi
}

report_go
report_rust

combine_reports() {
  local go_file="${OUT_DIR}/go-licenses.csv"
  local rust_file="${OUT_DIR}/rust-licenses.json"
  local combined="${OUT_DIR}/all-licenses.csv"

  if ! has_cmd python3; then
    echo "python3 not found; skipping combined report."
    return
  fi

  python3 - <<'PY'
import csv
import os
import json

out_dir = os.environ.get("OUT_DIR", "")
go_file = os.path.join(out_dir, "go-licenses.csv")
rust_file = os.path.join(out_dir, "rust-licenses.json")
combined = os.path.join(out_dir, "all-licenses.csv")

def extract_license_value(row):
    for key in row.keys():
        key_l = key.lower()
        if key_l in ("license", "licenses"):
            return row[key]
    for key in row.keys():
        if "license" in key.lower():
            return row[key]
    return None

licenses = {}

if os.path.exists(go_file):
    with open(go_file, newline="") as f:
        reader = csv.reader(f)
        for row in reader:
            if len(row) < 3:
                continue
            lic = (row[2] or "").strip()
            if not lic:
                continue
            licenses.setdefault(lic, set()).add("go")

if os.path.exists(rust_file):
    with open(rust_file) as f:
        try:
            rust_deps = json.load(f)
        except json.JSONDecodeError:
            rust_deps = []
        if isinstance(rust_deps, list):
            for dep in rust_deps:
                if not isinstance(dep, dict):
                    continue
                lic = dep.get("license") or dep.get("licenses")
                if not lic:
                    continue
                lic = str(lic).strip()
                if not lic:
                    continue
                licenses.setdefault(lic, set()).add("rust")

with open(combined, "w", newline="") as f:
    writer = csv.writer(f)
    writer.writerow(["license", "languages"])
    for lic in sorted(licenses.keys()):
        langs = ",".join(sorted(licenses[lic]))
        writer.writerow([lic, langs])

print(f"Combined license report written to {combined}")
PY
}

fetch_go_licenses() {
  local go_file="${OUT_DIR}/go-licenses.csv"
  local cache_dir="${OUT_DIR}/cache/go"
  local overrides_file="${OUT_DIR}/license-overrides.csv"

  if [[ "${FETCH_LICENSES}" != "true" ]]; then
    return
  fi

  if ! scan_enabled "go"; then
    return
  fi

  if [[ ! -f "${go_file}" ]]; then
    return
  fi

  if ! has_cmd curl; then
    echo "curl not found; skipping license fetch."
    return
  fi

  mkdir -p "${cache_dir}"

  python3 - <<'PY'
import csv
import os
import re
import subprocess
from urllib.parse import urlparse

out_dir = os.environ.get("OUT_DIR", "")
go_file = os.path.join(out_dir, "go-licenses.csv")
cache_dir = os.path.join(out_dir, "cache", "go")
overrides_file = os.path.join(out_dir, "license-overrides.csv")

if not os.path.exists(go_file):
    raise SystemExit(0)

def safe_name(s):
    return re.sub(r"[^A-Za-z0-9._-]+", "_", s)

overrides = {}
if os.path.exists(overrides_file):
    with open(overrides_file, newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            orig = (row.get("original_url") or "").strip()
            over = (row.get("override_url") or "").strip()
            if orig and over:
                overrides[orig] = over

with open(go_file, newline="") as f:
    reader = csv.reader(f)
    for row in reader:
        if len(row) < 2:
            continue
        module, url = row[0].strip(), row[1].strip()
        if not url.startswith("http"):
            continue
        url = overrides.get(url, url)
        parsed = urlparse(url)
        host = parsed.netloc.replace(":", "_")
        path = parsed.path.strip("/").replace("/", "_")
        fname = safe_name(f"{module}__{host}__{path}.txt")
        dest = os.path.join(cache_dir, fname)
        if os.path.exists(dest) and os.path.getsize(dest) > 0:
            continue
        try:
            subprocess.run(
                ["curl", "-fsSL", url, "-o", dest],
                check=True,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            print(f"Fetched {url}")
        except subprocess.CalledProcessError:
            if os.path.exists(dest):
                os.remove(dest)
            print(f"Failed to fetch {url}")
PY
}

fetch_go_licenses

combine_reports
