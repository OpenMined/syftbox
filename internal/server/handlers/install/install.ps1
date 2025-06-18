#!/usr/bin/env pwsh
# Licensed under the Apache License, Version 2.0

# --no-prompt => disables the run client prompt
$AskRunClient = $true

# --run => disables the prompt & runs the client
$RunClient = $false

$AppName = "syftbox"
$ArtifactBaseUrl = if ($env:ARTIFACT_BASE_URL) { $env:ARTIFACT_BASE_URL } else { "https://syftbox.net" }
$ArtifactDownloadUrl = "$ArtifactBaseUrl/releases"

function Write-Error-Exit($message) {
    Write-Host "ERROR: $message" -ForegroundColor Red
    exit 1
}

function Write-Info($message) {
    Write-Host $message -ForegroundColor Cyan
}

function Write-Warning-Custom($message) {
    Write-Host $message -ForegroundColor Yellow
}

function Write-Success($message) {
    Write-Host $message -ForegroundColor Green
}

function Test-Command($command) {
    return Get-Command $command -ErrorAction SilentlyContinue
}

function Need-Command($command) {
    if (-not (Test-Command $command)) {
        Write-Error-Exit "need '$command' (command not found)"
    }
}

###################################################

function Get-FileFromUrl($url, $outputFile) {
    try {
        Invoke-WebRequest -Uri $url -OutFile $outputFile
    }
    catch {
        Write-Error-Exit "Failed to download from $url. $($_.Exception.Message)"
    }
}

###################################################

function Add-ToPath($pathToAdd) {
    $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($userPath -notlike "*$pathToAdd*") {
        [Environment]::SetEnvironmentVariable("PATH", "$pathToAdd;$userPath", "User")
        $env:PATH = "$pathToAdd;$env:PATH"
        return $true
    }
    return $false
}

###################################################

# Detect architecture
function Detect-Arch {
    $arch = [System.Environment]::GetEnvironmentVariable("PROCESSOR_ARCHITECTURE")
    
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default {
            Write-Error-Exit "Unsupported architecture: $arch"
            exit 1
        }
    }
}

###################################################

function Prompt-RestartShell {
    Write-Host
    Write-Warning-Custom "RESTART your PowerShell or open a new PowerShell window to use syftbox"
    
    Write-Success "`nAfter restarting, start the client"
    Write-Host "  syftbox"
}

###################################################
# Download & Install SyftBox
# 
# Packages
# syftbox_client_windows_arm64.zip
# syftbox_client_windows_amd64.zip

function Run-Client {
    Write-Host
    Write-Success "Starting SyftBox client..."
    & "$env:USERPROFILE\.local\bin\syftbox.exe"
}

function Prompt-RunClient {
    Write-Host
    $startClient = Read-Host "Start the client now? [y/n]"
    
    if ($startClient -eq "y" -or $startClient -eq "Y") {
        Run-Client
    }
    else {
        Prompt-RestartShell
    }
}

function Uninstall-OldVersion {
    $syftboxPath = (Get-Command syftbox -ErrorAction SilentlyContinue).Path
    if ($syftboxPath) {
        Write-Info "Found old version of SyftBox ($syftboxPath). Removing..."
        
        # Just remove the file directly
        Remove-Item -Force $syftboxPath -ErrorAction SilentlyContinue
        Remove-Item -Force "$env:USERPROFILE\.local\bin\syftbox.exe" -ErrorAction SilentlyContinue
    }
}

function Pre-Install {
    Need-Command "powershell"
    
    Uninstall-OldVersion
}

function Post-Install {
    if (Test-Path "$env:USERPROFILE\.local\bin\syftbox.exe") {
        $version = & "$env:USERPROFILE\.local\bin\syftbox.exe" -v
        Write-Success "Installation completed! $version"
    } else {
        Write-Success "Installation completed!"
    }

    if ($RunClient) {
        Run-Client
    }
    elseif ($AskRunClient) {
        Prompt-RunClient
    }
    else {
        Prompt-RestartShell
    }
}

function Install-SyftBox {
    $arch = Detect-Arch
    $pkgName = "${AppName}_client_windows_${arch}"
    $tempDir = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString())
    
    Write-Info "Downloading SyftBox"
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    $downloadPath = "$tempDir\$pkgName.zip"
    Get-FileFromUrl "$ArtifactDownloadUrl/$pkgName.zip" $downloadPath
    
    Write-Info "Installing SyftBox"
    Expand-Archive -Path $downloadPath -DestinationPath $tempDir
    
    $localBinDir = "$env:USERPROFILE\.local\bin"
    New-Item -ItemType Directory -Path $localBinDir -Force | Out-Null
    
    Copy-Item -Path "$tempDir\$pkgName\syftbox.exe" -Destination "$localBinDir\syftbox.exe" -Force
    
    Remove-Item -Recurse -Force $tempDir
    
    $pathAdded = Add-ToPath $localBinDir
    if ($pathAdded) {
        Write-Info "Added $localBinDir to PATH"
    }
}

function Do-Install {
    param (
        [Parameter(ValueFromRemainingArguments=$true)]
        $args
    )
    
    foreach ($arg in $args) {
        switch ($arg) {
            { $_ -eq "-r" -or $_ -eq "--run" -or $_ -eq "run" } {
                $script:RunClient = $true
                break
            }
            { $_ -eq "-n" -or $_ -eq "--no-prompt" -or $_ -eq "no-prompt" } {
                $script:AskRunClient = $false
                break
            }
        }
    }
    
    Pre-Install
    Install-SyftBox
    Post-Install
}

try {
    Do-Install $args
}
catch {
    Write-Error-Exit ("Installation failed: " + $_.Exception.Message)
} 
