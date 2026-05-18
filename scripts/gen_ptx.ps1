# Generate PTX from poh_search.cu (requires CUDA Toolkit nvcc on PATH).
# Example: .\scripts\gen_ptx.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$src = Join-Path $root "kernels\poh_search.cu"
$out = Join-Path $root "internal\gpupoh\poh_search.ptx"
& nvcc $src -ptx -o $out -arch=sm_75
Write-Host "Wrote $out"
