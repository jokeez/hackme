# Run from repo: scp to C:\HackMe and execute once.
$ErrorActionPreference = "Stop"
$dir = "C:\HackMe"
Set-Location $dir
& "$dir\write_hackme_env.ps1" -InstallDir $dir -GpuBackend opencl -RigProfile amd_rx580_2048sp -NonInteractive
$seed = (Get-Content "$dir\data\node_ed25519.seed" -Raw).Trim()
$tokLine = Get-Content "$dir\hackme.env" | Where-Object { $_ -match '^HACKME_POOL_COORDINATOR_TOKEN=' } | Select-Object -First 1
$tok = ($tokLine -replace '^HACKME_POOL_COORDINATOR_TOKEN=', '').Trim()
if ($tok.Length -ne 64) { throw "bad pool token length $($tok.Length)" }
if ($seed.Length -ne 64) { throw "bad seed length $($seed.Length)" }
Stop-Process -Name workerpoh-opencl, workerpoh, hackme -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
Start-Process "$dir\hackme.exe" -WorkingDirectory $dir -WindowStyle Minimized
Start-Sleep -Seconds 14
New-Item -ItemType Directory -Force -Path "$dir\logs" | Out-Null
$log = "$dir\logs\worker-opencl-live.log"
$ocl = "$dir\workerpoh-opencl.exe"
$argList = @(
    '-coord', 'https://hackme.tech/pool/coordinator',
    '-token', $tok,
    '-worker', 'worker-desktop-1rgp4ge',
    '-batch', '1048576',
    '-gpu-chunk', '524288',
    '-search-timeout-ms', '4500',
    '-gpu-backend', 'opencl'
)
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $ocl
$psi.WorkingDirectory = $dir
$psi.Arguments = ($argList | ForEach-Object { if ($_ -match '\s') { """$_""" } else { $_ } }) -join ' '
$psi.UseShellExecute = $false
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.CreateNoWindow = $true
$env:HACKME_MINER_ED25519_SEED_HEX = $seed
$env:HACKME_WORKER_SIGN_SUBMITS = '1'
$env:HACKME_WORKER_CLAIM_COOLDOWN_MS = '28000'
$p = [System.Diagnostics.Process]::Start($psi)
Start-Sleep -Seconds 30
if (-not $p.HasExited) {
    "RUNNING pid=$($p.Id)" | Out-File -FilePath $log -Encoding ascii -Append
} else {
    "EXITED code=$($p.ExitCode)" | Out-File -FilePath $log -Encoding ascii -Append
    $out = $p.StandardOutput.ReadToEnd()
    $err = $p.StandardError.ReadToEnd()
    $out | Out-File -FilePath $log -Encoding ascii -Append
    $err | Out-File -FilePath ($log + '.err') -Encoding ascii -Append
}
Get-Content $log -Tail 20 -ErrorAction SilentlyContinue
Get-Process workerpoh-opencl -ErrorAction SilentlyContinue | Format-Table Name, Id
