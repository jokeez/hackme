# Build workerpoh-opencl.exe on Windows (AMD OpenCL). Requires gcc (MinGW) + CGO.
param(
    [string]$OutDir = $PSScriptRoot,
    [string]$HeadersDir = ""
)

$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$out = Join-Path $OutDir "workerpoh-opencl.exe"

function Ensure-OpenCLHeaders {
    param([string]$Dest)
    $cl = Join-Path $Dest "CL\cl.h"
    if (Test-Path $cl) { return $Dest }
    New-Item -ItemType Directory -Path (Join-Path $Dest "CL") -Force | Out-Null
    $base = "https://raw.githubusercontent.com/KhronosGroup/OpenCL-Headers/main/CL"
    foreach ($f in @("cl.h", "cl_platform.h", "cl_version.h")) {
        $url = "$base/$f"
        $path = Join-Path $Dest "CL\$f"
        Write-Host "[opencl] fetch $url"
        Invoke-WebRequest -Uri $url -OutFile $path -UseBasicParsing
    }
    return $Dest
}

if (-not $HeadersDir) {
    $HeadersDir = Join-Path $env:LOCALAPPDATA "HackMe-opencl-headers"
}
$HeadersDir = Ensure-OpenCLHeaders -Dest $HeadersDir

$env:CGO_ENABLED = "1"
$env:CGO_CFLAGS = "-I$HeadersDir"
# OpenCL.dll ships with AMD/NVIDIA/Intel drivers (System32).
$env:CGO_LDFLAGS = "-lOpenCL"

Push-Location $root
try {
    Write-Host "[opencl] go build -tags opencl -> $out"
    go build -tags opencl -trimpath -ldflags "-s -w" -o $out ./cmd/workerpoh
    Write-Host "[opencl] OK: $out"
} finally {
    Pop-Location
}
