# Patch hackme.env and restart node + OpenCL worker (RX 580).
$ErrorActionPreference = "Stop"
$dir = if ($args[0]) { $args[0] } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
if (-not (Test-Path (Join-Path $dir "hackme.exe"))) {
    $dir = "C:\Program Files\HackMe"
}
Set-Location $dir
$envf = Join-Path $dir "hackme.env"
$lines = @(Get-Content $envf | Where-Object { $_ -notmatch '^\s*HACKME_CUDA_CALIBRATE_GHS=' })
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
        $out.Add('HACKME_WORKER_CLAIM_COOLDOWN_MS=28000'); $hadCooldown = $true
    } else {
        $out.Add($l)
    }
}
if (-not $hadBackend) { $out.Add('HACKME_GPU_BACKEND=opencl') }
if (-not $hadBind) { $out.Add('HACKME_BIND_ADDR=127.0.0.1:8080') }
if (-not $hadCooldown) { $out.Add('HACKME_WORKER_CLAIM_COOLDOWN_MS=28000') }
if (-not ($out -match '^\s*HACKME_WORKER_SIGN_SUBMITS=')) { $out.Add('HACKME_WORKER_SIGN_SUBMITS=1') }
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
$env:HACKME_WORKER_CLAIM_COOLDOWN_MS = '28000'
$env:HACKME_WORKER_SIGN_SUBMITS = '1'
$logDir = Join-Path $dir 'logs'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$liveLog = Join-Path $logDir 'worker-opencl-live.log'
$wid = 'worker-desktop-1rgp4ge'
foreach ($l in $out) {
    if ($l -match '^\s*WORKER_ID=') { $wid = ($l -replace 'WORKER_ID=','').Trim(); break }
}
$env:HACKME_WORKER_CLAIM_COOLDOWN_MS = '28000'
$env:HACKME_WORKER_SIGN_SUBMITS = '1'
Start-Process $ocl -WorkingDirectory $dir -WindowStyle Minimized -ArgumentList @(
    '-coord', 'https://hackme.tech/pool/coordinator',
    '-token', $poolTok,
    '-worker', $wid,
    '-batch', '2097152',
    '-gpu-backend', 'opencl'
)
Start-Sleep -Seconds 10
Get-Process hackme,workerpoh* -ErrorAction SilentlyContinue | Format-Table Name, Id -AutoSize
