# Patch hackme.env and restart node + OpenCL worker (RX 580).
$ErrorActionPreference = "Stop"
$dir = if ($args[0]) { $args[0] } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
if (-not (Test-Path (Join-Path $dir "hackme.exe"))) {
    $dir = "C:\Program Files\HackMe"
}
Set-Location $dir
$envf = Join-Path $dir "hackme.env"
$lines = @(Get-Content $envf | Where-Object { $_ -notmatch '^\s*HACKME_CUDA_CALIBRATE_GHS=' })
$rigProfile = ''
foreach ($l in $lines) {
    if ($l -match '^\s*HACKME_RIG_PROFILE=(.+)') { $rigProfile = $Matches[1].Trim() }
}
# RX 580 profiles need 28s claim cooldown; NVIDIA / other rigs use 0 (Linux desktop fair pool).
$claimMs = if ($rigProfile -match '^amd_rx580') { '28000' } else { '0' }
$out = [System.Collections.Generic.List[string]]::new()
$hadBackend = $false
$hadBind = $false
$hadCooldown = $false
foreach ($l in $lines) {
    if ($l -match '^\s*HACKME_GPU_BACKEND=') {
        $out.Add('HACKME_GPU_BACKEND=opencl'); $hadBackend = $true
    } elseif ($l -match '^\s*HACKME_BIND_ADDR=') {
        $out.Add('HACKME_BIND_ADDR=127.0.0.1:8080'); $hadBind = $true
    } elseif ($l -match '^\s*HACKME_WORKER_CLAIM_COOLDOWN_MS=') {
        $out.Add("HACKME_WORKER_CLAIM_COOLDOWN_MS=$claimMs"); $hadCooldown = $true
    } else {
        $out.Add($l)
    }
}
if (-not $hadBackend) { $out.Add('HACKME_GPU_BACKEND=opencl') }
if (-not $hadBind) { $out.Add('HACKME_BIND_ADDR=127.0.0.1:8080') }
if (-not $hadCooldown) { $out.Add("HACKME_WORKER_CLAIM_COOLDOWN_MS=$claimMs") }
if (-not ($out -match '^\s*HACKME_WORKER_SIGN_SUBMITS=')) { $out.Add('HACKME_WORKER_SIGN_SUBMITS=1') }
$hostWid = 'worker-' + (($env:COMPUTERNAME).ToLower() -replace '[^a-z0-9-]', '-')
if (-not ($out -match '^\s*WORKER_ID=')) { $out.Add("WORKER_ID=$hostWid") }
$batchArg = '1048576'
$chunkArg = '524288'
$searchMs = '4500'
foreach ($l in $out) {
    if ($l -match '^\s*HACKME_WORKER_BATCH_SIZE=(\d+)') { $batchArg = $Matches[1] }
    if ($l -match '^\s*GPU_CHUNK=(\d+)') { $chunkArg = $Matches[1] }
    if ($l -match '^\s*SEARCH_TIMEOUT_MS=(\d+)') { $searchMs = $Matches[1] }
    if ($l -match '^\s*WORKER_ID=(.+)') { $hostWid = $Matches[1].Trim() }
}
[System.IO.File]::WriteAllLines($envf, $out.ToArray())
Write-Host "patched $envf"
$poolTok = (Get-Content $envf | Where-Object { $_ -match '^\s*HACKME_POOL_COORDINATOR_TOKEN=' }) -replace 'HACKME_POOL_COORDINATOR_TOKEN=',''
$poolTok = $poolTok.Trim().Trim('"')
Stop-Process -Name hackme,workerpoh,workerpoh-opencl -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 3
Start-Process (Join-Path $dir 'hackme.exe') -WorkingDirectory $dir -WindowStyle Minimized
Start-Sleep -Seconds 18
$ocl = Join-Path $dir 'workerpoh-opencl.exe'
if (-not (Test-Path $ocl)) { $ocl = Join-Path $dir 'workerpoh.exe' }
$seedPath = Join-Path $dir 'data\node_ed25519.seed'
if (-not (Test-Path $seedPath)) {
    Write-Error "missing node_ed25519.seed in data dir"
}
$seed = (Get-Content $seedPath -Raw).Trim().ToLower()
if ($seed.Length -ne 64) { Write-Error "node_ed25519.seed invalid length" }
$env:HACKME_MINER_ED25519_SEED_HEX = $seed
$env:HACKME_WORKER_CLAIM_COOLDOWN_MS = $claimMs
$env:HACKME_WORKER_SIGN_SUBMITS = '1'
$logDir = Join-Path $dir 'logs'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$liveLog = Join-Path $logDir 'worker-opencl-live.log'
$wid = $hostWid
$env:HACKME_WORKER_CLAIM_COOLDOWN_MS = $claimMs
$env:HACKME_WORKER_SIGN_SUBMITS = '1'
$env:HACKME_GPU_BACKEND = 'opencl'
$argList = @(
    '-coord', 'https://hackme.tech/pool/coordinator',
    '-token', $poolTok,
    '-worker', $wid,
    '-batch', $batchArg,
    '-gpu-chunk', $chunkArg,
    '-search-timeout-ms', $searchMs,
    '-gpu-backend', 'opencl'
)
Start-Process $ocl -WorkingDirectory $dir -WindowStyle Minimized -ArgumentList $argList
Start-Sleep -Seconds 25
if (Test-Path $liveLog) { Get-Content $liveLog -Tail 8 }
Get-Process hackme,workerpoh* -ErrorAction SilentlyContinue | Format-Table Name, Id -AutoSize
