# Detect primary GPU and suggest HackMe worker backend / rig profile.
param(
    [string]$OutFile = ""
)

$ErrorActionPreference = "SilentlyContinue"
$names = @()
try {
    $names = @(Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name } | Where-Object { $_ -and $_ -notmatch 'Microsoft Basic' })
} catch {
    $wmic = & wmic path win32_VideoController get name 2>$null
    if ($wmic) {
        $names = @($wmic | Select-Object -Skip 1 | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    }
}

$combined = ($names -join " ").ToLower()
$suggestBackend = "auto"
$rigProfile = ""
$vendor = "Unknown"
$hybrid = $false
$tips = @()

$hasNvidia = $combined -match 'nvidia|geforce|quadro|tesla|rtx|gtx'
$hasAmd = $combined -match 'amd|radeon'
if ($hasNvidia -and $hasAmd) {
    $hybrid = $true
    $vendor = "NVIDIA+AMD"
    $suggestBackend = "cuda"
    $tips += "Hybrid rig: fleet mode runs CUDA on NVIDIA + OpenCL on AMD (HACKME_GPU_FLEET=1, HACKME_GPU_HYBRID=auto)."
}
elseif ($hasNvidia) {
    $vendor = "NVIDIA"
    $suggestBackend = "cuda"
    $tips += "NVIDIA on Windows: OpenCL worker only (~9 GH/s). For full CUDA speed (~60+ GH/s) use HackMe OS or Linux bundle."
} elseif ($hasAmd) {
    $vendor = "AMD"
    $suggestBackend = "opencl"
    if ($combined -match '580' -and $combined -match '2048') {
        $rigProfile = "amd_rx580_2048sp"
        $tips += "RX 580 2048SP - daily rig profile (conservative batch, thermal guard)."
    } elseif ($combined -match '580') {
        $rigProfile = "amd_rx580_generic"
        $tips += "RX 580 - generic AMD profile."
    } else {
        $tips += "AMD GPU - OpenCL/auto; tune in AMD Adrenalin."
    }
} elseif ($combined -match 'intel.*arc') {
    $vendor = "Intel"
    $suggestBackend = "opencl"
    $tips += "Intel Arc - OpenCL/auto backend."
}

if (-not $names.Count) {
    $suggestBackend = "auto"
    $tips += "No discrete GPU detected - CPU mining (low GH/s)."
}

$obj = [ordered]@{
    gpu_names       = $names
    vendor          = $vendor
    hybrid          = $hybrid
    suggest_backend = $suggestBackend
    rig_profile     = $rigProfile
    tips            = $tips
    summary         = if ($names.Count) { ($names -join "; ") } else { "No GPU detected" }
}

$json = $obj | ConvertTo-Json -Compress
if ($OutFile) {
    $dir = Split-Path -Parent $OutFile
    if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    [System.IO.File]::WriteAllText($OutFile, $json, [System.Text.UTF8Encoding]::new($false))
} else {
    Write-Output $json
}
