# HackMe fuzzing CLI — Windows quick start (run in PowerShell after hackme-node is up on :8080).
# Usage: powershell -ExecutionPolicy Bypass -File fuzzing_windows_quickstart.ps1
# Optional: -InstallDir C:\HackMe -ReleaseVer 0.1.0-rc11n
param(
    [string]$InstallDir = "$env:USERPROFILE\HackMe",
    [string]$ReleaseVer = "0.1.0-rc11n",
    [string]$Base = "http://127.0.0.1:8080"
)

$ErrorActionPreference = "Stop"
$DistBase = "https://hackme.tech/dist/release_$ReleaseVer"
$Cli = Join-Path $InstallDir "hackme-fuzzing-$ReleaseVer-windows-amd64.exe"
$Build = Join-Path $InstallDir "hackme-fuzzing-build-$ReleaseVer-windows-amd64.exe"

Write-Host "=== HackMe fuzzing CLI (Windows) ===" -ForegroundColor Cyan
Write-Host "Install dir: $InstallDir"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

foreach ($pair in @(
    @("$DistBase/hackme-fuzzing-$ReleaseVer-windows-amd64.exe", $Cli),
    @("$DistBase/hackme-fuzzing-build-$ReleaseVer-windows-amd64.exe", $Build)
)) {
    $url, $dest = $pair
    if (-not (Test-Path $dest)) {
        Write-Host "Downloading $(Split-Path $dest -Leaf) ..."
        Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing
    }
    Unblock-File -Path $dest -ErrorAction SilentlyContinue
}

try {
    Invoke-WebRequest -Uri "$Base/api/status?lite=1" -UseBasicParsing -TimeoutSec 5 | Out-Null
} catch {
    Write-Host "ERROR: Local node not reachable at $Base" -ForegroundColor Red
    Write-Host "Start hackme-node first (HackMe Miner from Start menu, or hackme.exe)."
    exit 1
}

$env:HACKME_FUZZING_BASE = $Base
$env:HACKME_FUZZING_BUILD = $Build

Write-Host "Registering developer token ..."
& $Cli register --base $Base --save
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "`nWallet:"
& $Cli wallet --base $Base
Write-Host "`nOK. Next: hackme-fuzzing tasks --base $Base" -ForegroundColor Green
