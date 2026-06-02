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
# Optional floor only when kernel timing fails — never pin pool GH/s (see HACKME_GPU_HASHRATE_FLOOR_GHS).
$floorGhs = ""

$gpuVendor = ""
$gpuJson = Join-Path $dir "gpu_detect.json"
if (-not $RigProfile -and (Test-Path $gpuJson)) {
    try {
        $g = Get-Content $gpuJson -Raw | ConvertFrom-Json
        if ($g.rig_profile) { $RigProfile = $g.rig_profile }
        if ($GpuBackend -eq "auto" -and $g.suggest_backend) { $GpuBackend = $g.suggest_backend }
        if ($g.vendor) { $gpuVendor = [string]$g.vendor }
    } catch { }
}

$hasOpenCLBin = Test-Path -LiteralPath (Join-Path -Path $dir -ChildPath "workerpoh-opencl.exe")
$hasCudaBin = Test-Path -LiteralPath (Join-Path -Path $dir -ChildPath "workerpoh-cuda.exe")

# NVIDIA desktop: align with Linux fair pool (no 28s claim sleep; longer GPU search window).
if (-not ($RigProfile -match '^amd_rx580') -and ($gpuVendor -match 'NVIDIA' -or $GpuBackend -eq 'cuda')) {
    $searchMs = 12000
    $claimMs = 0
    if ($hasCudaBin) {
        $GpuBackend = 'cuda'
        Write-Host 'NVIDIA: workerpoh-cuda.exe (CUDA with automatic OpenCL fallback if NVRTC/driver mismatch).'
    } elseif ($hasOpenCLBin) {
        $GpuBackend = 'opencl'
        Write-Host 'NVIDIA on Windows: OpenCL worker (workerpoh-opencl.exe). For native CUDA use HackMe OS ISO or Linux bundle (rc11j).'
    }
}

switch ($RigProfile) {
    "amd_rx580_2048sp" {
        if ($GpuBackend -eq "cuda") { $GpuBackend = "auto" }
        if ($hasOpenCLBin -and ($GpuBackend -eq "auto" -or $GpuBackend -eq "opencl")) { $GpuBackend = "opencl" }
        $batch = 1048576; $chunk = 524288; $searchMs = 4500; $claimMs = 28000
        $tempPause = 78; $tempResume = 72
        # No fixed CALIBRATE_GHS — worker measures OpenCL kernel time.
    }
    "amd_rx580_generic" {
        if ($hasOpenCLBin -and ($GpuBackend -eq "auto" -or $GpuBackend -eq "opencl")) { $GpuBackend = "opencl" }
        $batch = 2097152; $chunk = 1048576; $searchMs = 4000; $claimMs = 28000
        $tempPause = 80; $tempResume = 74
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

$hostWid = 'worker-' + (($env:COMPUTERNAME).ToLower() -replace '[^a-z0-9-]', '-')
$hostWid = $hostWid.Trim('-')
if (-not $hostWid -or $hostWid -eq 'worker-') { $hostWid = 'worker-desktop' }

$lines = @(
    "HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech",
    "HACKME_ADMIN_TOKEN=$admin",
    "HACKME_POOL_COORDINATOR_TOKEN=$poolToken",
    "HACKME_REQUIRE_ADMIN_TOKEN=1",
    "HACKME_DESKTOP_MODE=1",
    "HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1",
    "HACKME_RIG_PROFILE_AUTO=0",
    "HACKME_GPU_BACKEND=$GpuBackend",
    "HACKME_WORKER_BATCH_SIZE=$batch",
    "GPU_CHUNK=$chunk",
    "SEARCH_TIMEOUT_MS=$searchMs",
    "HACKME_WORKER_CLAIM_COOLDOWN_MS=$claimMs",
    "HACKME_GPU_TEMP_PAUSE_C=$tempPause",
    "HACKME_GPU_TEMP_RESUME_C=$tempResume",
    "HACKME_DESKTOP_GPU_POOL=1",
    "HACKME_BIND_ADDR=127.0.0.1:8080",
    "HACKME_WORKER_SIGN_SUBMITS=1",
    "WORKER_ID=$hostWid",
    "HACKME_WORKER_WATCHDOG=1",
    "HACKME_WORKER_WATCHDOG_SEC=45",
    "HACKME_POOL_COORDINATOR_URL=https://hackme.tech/pool/coordinator"
)
if ($RigProfile) { $lines += "HACKME_RIG_PROFILE=$RigProfile" }
if ($floorGhs) { $lines += "HACKME_GPU_HASHRATE_FLOOR_GHS=$floorGhs" }
if ($gpuDisable) { $lines += "HACKME_GPU_DISABLE=$gpuDisable" }
# Hard-lock AMD RX profiles: never leave auto/150ms cooldown from stale merges.
if ($RigProfile -match '^amd_rx580') {
    $lines = @($lines | Where-Object { $_ -notmatch '^(HACKME_GPU_BACKEND|HACKME_WORKER_CLAIM_COOLDOWN_MS|HACKME_CUDA_CALIBRATE_GHS)=' })
    $lines += 'HACKME_GPU_BACKEND=opencl'
    $lines += 'HACKME_WORKER_CLAIM_COOLDOWN_MS=28000'
}

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
