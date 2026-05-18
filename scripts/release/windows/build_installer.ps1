# Build HackMe-Setup-<version>.exe with Inno Setup 6.
# Prerequisites (Windows): Inno Setup in PATH (iscc.exe), release bundle already built:
#   VERSION=0.1.0-rc8 bash scripts/release/make_release_bundle.sh
#
# Usage (PowerShell, repo root):
#   pwsh -File scripts/release/windows/build_installer.ps1 -Version 0.1.0-rc8

param(
    [Parameter(Mandatory = $false)]
    [string] $Version = "0.1.0-rc8"
)

$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$Iss = Join-Path $Root "scripts\release\windows\hackme.iss"
$DistExe = Join-Path $Root "dist\release_$Version\windows\hackme.exe"

if (-not (Get-Command iscc -ErrorAction SilentlyContinue)) {
    Write-Error "iscc not in PATH. Install Inno Setup 6 and add its folder to PATH (e.g. C:\Program Files (x86)\Inno Setup 6)."
}
if (-not (Test-Path $Iss)) { Write-Error "Missing iss: $Iss" }
if (-not (Test-Path $DistExe)) {
    Write-Error "Missing $DistExe — run from repo root: VERSION=$Version bash scripts/release/make_release_bundle.sh"
}

Set-Location $Root
Write-Host "[inno] iscc /DMyAppVersion=$Version $Iss"
& iscc "/DMyAppVersion=$Version" $Iss
Write-Host "[inno] done. Output is under dist\release_$Version\windows\ (HackMe-Setup-$Version.exe) or Inno OutputDir if customized."
