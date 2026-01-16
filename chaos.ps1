param(
    [switch]$Go,
    [switch]$Rust
)

if (-not $Go -and -not $Rust) {
    if ($args -contains '--go' -or $args -contains '-go' -or $args -contains '/go') {
        $Go = $true
    }
    if ($args -contains '--rust' -or $args -contains '-rust' -or $args -contains '/rust') {
        $Rust = $true
    }
}

if ($Go -and $Rust) {
    Write-Error "Choose either --go or --rust, not both."
    exit 2
}

if (-not $Go -and -not $Rust) {
    Write-Error "Usage: .\\chaos.ps1 --go | --rust"
    exit 2
}

function Get-GitBashPath {
    $gitRoots = @(
        (Join-Path $env:ProgramFiles "Git"),
        (Join-Path ${env:ProgramFiles(x86)} "Git")
    ) | Where-Object { $_ -and (Test-Path $_) }

    foreach ($root in $gitRoots) {
        $cygpath = Join-Path $root "usr\bin\cygpath.exe"
        $bashBin = Join-Path $root "bin\bash.exe"
        $bashUsr = Join-Path $root "usr\bin\bash.exe"
        if ((Test-Path $bashBin) -and (Test-Path $cygpath)) {
            return $bashBin
        }
        if ((Test-Path $bashUsr) -and (Test-Path $cygpath)) {
            return $bashUsr
        }
    }

    return $null
}

$bash = Get-GitBashPath
if (-not $bash) {
    Write-Error "Git Bash not found. Install Git for Windows and try again."
    exit 1
}

$mode = if ($Rust) { "rust" } else { "go" }
$repoRoot = (Resolve-Path -LiteralPath $PSScriptRoot).Path

$env:SBDEV_REPO_ROOT = $repoRoot
$env:SBDEV_TEST_MODE = $mode

$bashScript = @'
set -euo pipefail
REPO_ROOT=$(cygpath -u "$SBDEV_REPO_ROOT")
cd "$REPO_ROOT"

MODE="$SBDEV_TEST_MODE"
if [ "$MODE" = "rust" ]; then
  echo "Building Rust client..."
  (cd rust && cargo build --release)
  if [ -f "rust/target/release/syftbox-rs.exe" ]; then
    RUST_BIN="rust/target/release/syftbox-rs.exe"
  else
    RUST_BIN="rust/target/release/syftbox-rs"
  fi
  RUST_BIN_PATH="$REPO_ROOT/$RUST_BIN"
  export SBDEV_CLIENT_MODE=rust
  export SBDEV_RUST_CLIENT_BIN="$(cygpath -w "$RUST_BIN_PATH")"
fi

CHAOS_DURATION=1m just sbdev-test-chaos
'@

& $bash -lc $bashScript
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Remove-Item Env:SBDEV_REPO_ROOT -ErrorAction SilentlyContinue
Remove-Item Env:SBDEV_TEST_MODE -ErrorAction SilentlyContinue
