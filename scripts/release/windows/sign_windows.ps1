param(
    [Parameter(Mandatory = $true)]
    [string]$FilePath,
    [Parameter(Mandatory = $true)]
    [string]$PfxPath,
    [Parameter(Mandatory = $true)]
    [string]$PfxPassword,
    [string]$TimestampUrl = "http://timestamp.digicert.com"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $FilePath)) {
    throw "File not found: $FilePath"
}
if (-not (Test-Path $PfxPath)) {
    throw "PFX not found: $PfxPath"
}

$signtool = Get-Command signtool.exe -ErrorAction SilentlyContinue
if (-not $signtool) {
    throw "signtool.exe not found. Install Windows SDK."
}

Write-Host "[sign] signing $FilePath"
& signtool.exe sign `
    /fd SHA256 `
    /f $PfxPath `
    /p $PfxPassword `
    /tr $TimestampUrl `
    /td SHA256 `
    $FilePath

Write-Host "[sign] verify"
& signtool.exe verify /pa /v $FilePath

Write-Host "[sign] done"
