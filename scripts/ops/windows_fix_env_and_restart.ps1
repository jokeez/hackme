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
foreach ($l in $lines) {
    if ($l -match '^\s*HACKME_GPU_BACKEND=') {
        $out.Add('HACKME_GPU_BACKEND=opencl'); $hadBackend = $true
    } elseif ($l -match '^\s*HACKME_BIND_ADDR=') {
        $out.Add('HACKME_BIND_ADDR=127.0.0.1:8080'); $hadBind = $true
    } else {
        $out.Add($l)
    }
}
if (-not $hadBackend) { $out.Add('HACKME_GPU_BACKEND=opencl') }
if (-not $hadBind) { $out.Add('HACKME_BIND_ADDR=127.0.0.1:8080') }
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
Start-Process $ocl -WorkingDirectory $dir -WindowStyle Minimized -ArgumentList @(
    '-coord', 'https://hackme.tech/pool/coordinator',
    '-token', $poolTok,
    '-worker', 'worker-desktop-1rgp4ge',
    '-batch', '2097152',
    '-gpu-backend', 'opencl'
)
Start-Sleep -Seconds 10
Get-Process hackme,workerpoh* -ErrorAction SilentlyContinue | Format-Table Name, Id -AutoSize
