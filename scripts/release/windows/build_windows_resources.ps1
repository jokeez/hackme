param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$IconPath = ""
)

$ErrorActionPreference = "Stop"
$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($IconPath)) {
    $candidateLocal = Join-Path $scriptRoot "hackme.ico"
    $candidateRepo = "scripts/release/windows/hackme.ico"
    if (Test-Path $candidateLocal) {
        $IconPath = $candidateLocal
    } else {
        $IconPath = $candidateRepo
    }
}

$goversioninfo = Get-Command goversioninfo -ErrorAction SilentlyContinue
if (-not $goversioninfo) {
    Write-Host "[res] installing goversioninfo..."
    go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
    $env:PATH += ";" + (go env GOPATH) + "\bin"
}

if (-not (Test-Path $IconPath)) {
    throw "Icon not found: $IconPath"
}

$templateLocal = Join-Path $scriptRoot "versioninfo.json.template"
$templateRepo = "scripts/release/windows/versioninfo.json.template"
$template = $templateRepo
if (Test-Path $templateLocal) {
    $template = $templateLocal
}
if (-not (Test-Path $template)) {
    throw "Template not found: $template"
}

$json = Get-Content $template -Raw
$json = $json.Replace("__VERSION__", $Version)
$json = $json.Replace("__ICON_PATH__", $IconPath.Replace("\", "/"))

$tmp = Join-Path $scriptRoot "versioninfo.generated.json"
$json | Set-Content -NoNewline $tmp -Encoding UTF8

Write-Host "[res] generating resource syso"
goversioninfo -64 -o "resource_windows_amd64.syso" $tmp

Write-Host "[res] done: resource_windows_amd64.syso"
