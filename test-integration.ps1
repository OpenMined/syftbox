param(
    [switch]$Go,
    [switch]$Rust,
    [switch]$Serial
)

if (-not $Go -and -not $Rust) {
    if ($args -contains '--go' -or $args -contains '-go' -or $args -contains '/go') {
        $Go = $true
    }
    if ($args -contains '--rust' -or $args -contains '-rust' -or $args -contains '/rust') {
        $Rust = $true
    }
}

if (-not $Serial) {
    if ($args -contains '--serial' -or $args -contains '--one-at-a-time' -or $args -contains '--single' -or $args -contains '-serial' -or $args -contains '/serial') {
        $Serial = $true
    }
}

if ($Go -and $Rust) {
    Write-Error "Choose either --go or --rust, not both."
    exit 2
}

if (-not $Go -and -not $Rust) {
    Write-Error "Usage: .\\test-integration.ps1 --go | --rust [--serial]"
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
$sbdevRecipe = if ($Serial) { "sbdev-test-all-serial" } else { "sbdev-test-all" }

$env:SBDEV_REPO_ROOT = $repoRoot
$env:SBDEV_TEST_MODE = $mode

$bashScript = @'
set -euo pipefail
REPO_ROOT=$(cygpath -u "$SBDEV_REPO_ROOT")
cd "$REPO_ROOT"
just __RECIPE__ mode=$SBDEV_TEST_MODE
'@ -replace '__RECIPE__', $sbdevRecipe

& $bash -lc $bashScript
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Remove-Item Env:SBDEV_REPO_ROOT -ErrorAction SilentlyContinue
Remove-Item Env:SBDEV_TEST_MODE -ErrorAction SilentlyContinue
