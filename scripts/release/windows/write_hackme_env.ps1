# Write hackme.env next to hackme.exe (ASCII, no BOM). Pool token from pool.miner.token is mandatory.
param(
    [Parameter(Mandatory = $true)][string]$InstallDir,
    [ValidateSet("auto", "cuda", "opencl", "cpu")][string]$GpuBackend = "auto",
    [string]$RigProfile = "",
    [switch]$NonInteractive,
    [switch]$RepairOnly
)

$ErrorActionPreference = "Stop"
# Batch "%~dp0" + closing quote turns trailing \ into an escaped quote - strip junk chars.
$dir = $InstallDir.Trim().Trim('"').TrimEnd([char]'\', '/')
if (-not $dir) { Write-Error "InstallDir is empty" }

$exePath = Join-Path -Path $dir -ChildPath "hackme.exe"
if (-not (Test-Path -LiteralPath $exePath)) {
    Write-Error "hackme.exe not found in $dir"
}

$poolFile = Join-Path -Path $dir -ChildPath "pool.miner.token"
if (-not (Test-Path -LiteralPath $poolFile)) {
    Write-Error "pool.miner.token missing in $dir - download a fresh installer from https://hackme.tech/downloads.html"
}
$poolToken = [System.IO.File]::ReadAllText($poolFile).Trim()
if (-not $poolToken -or $poolToken -eq "REPLACE_WITH_POOL_TOKEN" -or $poolToken -match 'REPLACE|YOUR_|TOKEN_HERE') {
    Write-Error "pool.miner.token is empty or placeholder - ask pool operator for a new release build."
}

$envPath = Join-Path $dir "hackme.env"
$admin = ""
if (Test-Path $envPath) {
    foreach ($line in [System.IO.File]::ReadAllLines($envPath)) {
        if ($line -match '^\s*HACKME_ADMIN_TOKEN=(.+)$') { $admin = $Matches[1].Trim(); break }
    }
}
if (-not $admin) {
    $b = New-Object byte[] 24
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
    $admin = ($b | ForEach-Object { $_.ToString("x2") }) -join ""
}

# Rig profile / batch defaults
$batch = 4194304
$chunk = 4194304
$searchMs = 2500
$claimMs = 0
$tempPause = 83
$tempResume = 76
$calibGhs = ""

if (-not $RigProfile) {
    $gpuJson = Join-Path $dir "gpu_detect.json"
    if (Test-Path $gpuJson) {
        try {
            $g = Get-Content $gpuJson -Raw | ConvertFrom-Json
            if ($g.rig_profile) { $RigProfile = $g.rig_profile }
            if ($GpuBackend -eq "auto" -and $g.suggest_backend) { $GpuBackend = $g.suggest_backend }
        } catch { }
    }
}

$hasOpenCLBin = Test-Path -LiteralPath (Join-Path -Path $dir -ChildPath "workerpoh-opencl.exe")

switch ($RigProfile) {
    "amd_rx580_2048sp" {
        if ($GpuBackend -eq "cuda") { $GpuBackend = "auto" }
        if ($hasOpenCLBin -and ($GpuBackend -eq "auto" -or $GpuBackend -eq "opencl")) { $GpuBackend = "opencl" }
        $batch = 1048576; $chunk = 524288; $searchMs = 4500; $claimMs = 200
        $tempPause = 78; $tempResume = 72
        $calibGhs = if ($hasOpenCLBin) { "3.5" } else { "0.12" }
    }
    "amd_rx580_generic" {
        if ($hasOpenCLBin -and ($GpuBackend -eq "auto" -or $GpuBackend -eq "opencl")) { $GpuBackend = "opencl" }
        $batch = 2097152; $chunk = 1048576; $searchMs = 4000; $claimMs = 150
        $tempPause = 80; $tempResume = 74
        $calibGhs = if ($hasOpenCLBin) { "4" } else { "0.2" }
    }
}

$gpuDisable = ""
if ($GpuBackend -eq "cpu") {
    $GpuBackend = "auto"
    $gpuDisable = "1"
    $batch = 1048576
}

if (-not (Test-Path -LiteralPath (Join-Path -Path $dir -ChildPath "logs"))) {
    New-Item -ItemType Directory -Path (Join-Path -Path $dir -ChildPath "logs") -Force | Out-Null
}
if (-not (Test-Path -LiteralPath (Join-Path -Path $dir -ChildPath "data"))) {
    New-Item -ItemType Directory -Path (Join-Path -Path $dir -ChildPath "data") -Force | Out-Null
}

$lines = @(
    "HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech",
    "HACKME_ADMIN_TOKEN=$admin",
    "HACKME_POOL_COORDINATOR_TOKEN=$poolToken",
    "HACKME_REQUIRE_ADMIN_TOKEN=1",
    "HACKME_DESKTOP_MODE=1",
    "HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1",
    "HACKME_RIG_PROFILE_AUTO=1",
    "HACKME_GPU_BACKEND=$GpuBackend",
    "HACKME_WORKER_BATCH_SIZE=$batch",
    "GPU_CHUNK=$chunk",
    "SEARCH_TIMEOUT_MS=$searchMs",
    "HACKME_WORKER_CLAIM_COOLDOWN_MS=$claimMs",
    "HACKME_GPU_TEMP_PAUSE_C=$tempPause",
    "HACKME_GPU_TEMP_RESUME_C=$tempResume",
    "HACKME_DESKTOP_GPU_POOL=1"
)
if ($RigProfile) { $lines += "HACKME_RIG_PROFILE=$RigProfile" }
if ($calibGhs) { $lines += "HACKME_CUDA_CALIBRATE_GHS=$calibGhs" }
if ($gpuDisable) { $lines += "HACKME_GPU_DISABLE=$gpuDisable" }

$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllLines($envPath, $lines, $utf8NoBom)

Write-Host "OK: $envPath"
Write-Host "Admin token (dashboard): $admin"
Write-Host "Pool token: configured ($($poolToken.Length) chars)"
Write-Host "GPU backend: $GpuBackend$(if ($RigProfile) { " rig $RigProfile" })"

if (-not $RepairOnly) {
    $desktopBat = Join-Path $env:USERPROFILE "Desktop\Start HackMe Miner.bat"
    $shortcut = @(
        "@echo off",
        "cd /d `"$dir`"",
        "call start_hackme_miner.bat"
    )
    [System.IO.File]::WriteAllLines($desktopBat, $shortcut, $utf8NoBom)
    Write-Host "Desktop shortcut: $desktopBat"
}
